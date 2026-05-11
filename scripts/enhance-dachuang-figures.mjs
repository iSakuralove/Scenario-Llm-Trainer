import { promises as fs } from "node:fs";
import { existsSync } from "node:fs";
import { createRequire } from "node:module";
import { dirname, extname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

function parseArgs(argv) {
  const options = {
    scale: 2,
    sharpen: 0.32,
    dpi: 300,
  };

  for (let i = 0; i < argv.length; i += 1) {
    const key = argv[i];
    const value = argv[i + 1];
    switch (key) {
      case "--input":
        options.input = value;
        i += 1;
        break;
      case "--output":
        options.output = value;
        i += 1;
        break;
      case "--scale":
        options.scale = Number(value);
        i += 1;
        break;
      case "--sharpen":
        options.sharpen = Number(value);
        i += 1;
        break;
      case "--dpi":
        options.dpi = Number(value);
        i += 1;
        break;
      default:
        throw new Error(`未知参数: ${key}`);
    }
  }

  if (!options.input) {
    throw new Error("缺少 --input 参数");
  }
  if (!options.output) {
    throw new Error("缺少 --output 参数");
  }
  if (!Number.isFinite(options.scale) || options.scale < 1) {
    throw new Error("scale 必须是大于等于 1 的数字");
  }
  if (!Number.isFinite(options.sharpen) || options.sharpen < 0) {
    throw new Error("sharpen 必须是大于等于 0 的数字");
  }
  if (!Number.isFinite(options.dpi) || options.dpi <= 0) {
    throw new Error("dpi 必须是正数");
  }

  return options;
}

function bufferToDataUrl(buffer) {
  return `data:image/png;base64,${buffer.toString("base64")}`;
}

function dataUrlToBuffer(dataUrl) {
  const marker = "base64,";
  const index = dataUrl.indexOf(marker);
  if (index === -1) {
    throw new Error("无法解析 data URL");
  }
  return Buffer.from(dataUrl.slice(index + marker.length), "base64");
}

function getComparisonSample(files) {
  const patterns = ["Router", "甘特图", "流程图"];
  for (const pattern of patterns) {
    const match = files.find((file) => file.name.includes(pattern));
    if (match) {
      return match;
    }
  }
  return files[0];
}

function buildCrcTable() {
  const table = new Uint32Array(256);
  for (let i = 0; i < 256; i += 1) {
    let c = i;
    for (let j = 0; j < 8; j += 1) {
      c = (c & 1) ? (0xedb88320 ^ (c >>> 1)) : (c >>> 1);
    }
    table[i] = c >>> 0;
  }
  return table;
}

const crcTable = buildCrcTable();

function crc32(buffer) {
  let crc = 0xffffffff;
  for (const byte of buffer) {
    crc = crcTable[(crc ^ byte) & 0xff] ^ (crc >>> 8);
  }
  return (crc ^ 0xffffffff) >>> 0;
}

function createPhysChunk(dpi) {
  const pixelsPerMeter = Math.round(dpi / 0.0254);
  const chunkType = Buffer.from("pHYs", "ascii");
  const chunkData = Buffer.alloc(9);
  chunkData.writeUInt32BE(pixelsPerMeter, 0);
  chunkData.writeUInt32BE(pixelsPerMeter, 4);
  chunkData.writeUInt8(1, 8);
  const length = Buffer.alloc(4);
  length.writeUInt32BE(chunkData.length, 0);
  const crc = Buffer.alloc(4);
  crc.writeUInt32BE(crc32(Buffer.concat([chunkType, chunkData])), 0);
  return Buffer.concat([length, chunkType, chunkData, crc]);
}

function applyPngDpi(pngBuffer, dpi) {
  const signature = pngBuffer.subarray(0, 8);
  const chunks = [signature];
  let offset = 8;
  let inserted = false;

  while (offset < pngBuffer.length) {
    const length = pngBuffer.readUInt32BE(offset);
    const type = pngBuffer.toString("ascii", offset + 4, offset + 8);
    const chunkEnd = offset + 12 + length;
    const chunk = pngBuffer.subarray(offset, chunkEnd);

    if (type === "IHDR") {
      chunks.push(chunk);
      chunks.push(createPhysChunk(dpi));
      inserted = true;
    } else if (type !== "pHYs") {
      chunks.push(chunk);
    }

    offset = chunkEnd;
  }

  if (!inserted) {
    throw new Error("PNG 缺少 IHDR，无法写入 DPI");
  }

  return Buffer.concat(chunks);
}

async function enhanceImage(page, inputDataUrl, scale, sharpen) {
  return page.evaluate(
    async ({ dataUrl, targetScale, sharpenStrength }) => {
      const loadImage = (src) =>
        new Promise((resolve, reject) => {
          const image = new Image();
          image.onload = () => resolve(image);
          image.onerror = () => reject(new Error("图片加载失败"));
          image.src = src;
        });

      const sharpenImageData = (imageData, strength) => {
        const { data, width, height } = imageData;
        const source = new Uint8ClampedArray(data);
        const output = new Uint8ClampedArray(data.length);
        output.set(data);
        const side = -strength;
        const center = 1 + 4 * strength;
        const rowStride = width * 4;

        for (let y = 1; y < height - 1; y += 1) {
          for (let x = 1; x < width - 1; x += 1) {
            const index = (y * width + x) * 4;
            for (let channel = 0; channel < 3; channel += 1) {
              const value =
                source[index + channel] * center +
                source[index - 4 + channel] * side +
                source[index + 4 + channel] * side +
                source[index - rowStride + channel] * side +
                source[index + rowStride + channel] * side;
              output[index + channel] = Math.max(0, Math.min(255, Math.round(value)));
            }
            output[index + 3] = source[index + 3];
          }
        }

        return new ImageData(output, width, height);
      };

      const image = await loadImage(dataUrl);
      const width = image.width * targetScale;
      const height = image.height * targetScale;
      const canvas = document.createElement("canvas");
      canvas.width = width;
      canvas.height = height;

      const context = canvas.getContext("2d", { willReadFrequently: true });
      context.imageSmoothingEnabled = true;
      context.imageSmoothingQuality = "high";
      context.fillStyle = "#ffffff";
      context.fillRect(0, 0, width, height);
      context.drawImage(image, 0, 0, width, height);

      if (sharpenStrength > 0) {
        const imageData = context.getImageData(0, 0, width, height);
        const sharpened = sharpenImageData(imageData, sharpenStrength);
        context.putImageData(sharpened, 0, 0);
      }

      return {
        width,
        height,
        dataUrl: canvas.toDataURL("image/png"),
      };
    },
    { dataUrl: inputDataUrl, targetScale: scale, sharpenStrength: sharpen },
  );
}

async function createComparison(page, originalDataUrl, enhancedDataUrl, scale) {
  return page.evaluate(
    async ({ sourceDataUrl, resultDataUrl, targetScale }) => {
      const loadImage = (src) =>
        new Promise((resolve, reject) => {
          const image = new Image();
          image.onload = () => resolve(image);
          image.onerror = () => reject(new Error("图片加载失败"));
          image.src = src;
        });

      const original = await loadImage(sourceDataUrl);
      const enhanced = await loadImage(resultDataUrl);
      const cropWidth = Math.min(Math.max(420, Math.floor(original.width / 3)), original.width - 20);
      const cropHeight = Math.min(Math.max(320, Math.floor(original.height / 3)), original.height - 20);
      const cropX = Math.max(0, Math.floor((original.width - cropWidth) / 2));
      const cropY = Math.max(0, Math.floor((original.height - cropHeight) / 2));
      const enhancedCropX = cropX * targetScale;
      const enhancedCropY = cropY * targetScale;
      const panelWidth = cropWidth * targetScale;
      const panelHeight = cropHeight * targetScale;
      const margin = 40;
      const labelHeight = 56;
      const canvas = document.createElement("canvas");
      canvas.width = panelWidth * 2 + margin * 3;
      canvas.height = panelHeight + margin * 2 + labelHeight + 36;

      const context = canvas.getContext("2d");
      context.fillStyle = "#ffffff";
      context.fillRect(0, 0, canvas.width, canvas.height);

      const leftX = margin;
      const topY = margin + labelHeight;
      const rightX = margin * 2 + panelWidth;

      context.fillStyle = "#212529";
      context.font = "bold 20px 'Microsoft YaHei UI', sans-serif";
      context.fillText("Original Crop", leftX, margin);
      context.fillText("Enhanced Crop", rightX, margin);

      context.fillStyle = "#6c757d";
      context.font = "14px 'Microsoft YaHei UI', sans-serif";
      context.fillText("Same center region, scaled for direct visual comparison.", margin, canvas.height - 18);

      context.fillStyle = "#f8f9fa";
      context.fillRect(leftX, topY, panelWidth, panelHeight);
      context.fillRect(rightX, topY, panelWidth, panelHeight);
      context.strokeStyle = "rgba(56,85,155,0.45)";
      context.lineWidth = 2;
      context.strokeRect(leftX, topY, panelWidth, panelHeight);
      context.strokeRect(rightX, topY, panelWidth, panelHeight);

      context.drawImage(original, cropX, cropY, cropWidth, cropHeight, leftX, topY, panelWidth, panelHeight);
      context.drawImage(
        enhanced,
        enhancedCropX,
        enhancedCropY,
        cropWidth * targetScale,
        cropHeight * targetScale,
        rightX,
        topY,
        panelWidth,
        panelHeight,
      );

      return canvas.toDataURL("image/png");
    },
    { sourceDataUrl: originalDataUrl, resultDataUrl: enhancedDataUrl, targetScale: scale },
  );
}

const options = parseArgs(process.argv.slice(2));
const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const inputDir = resolve(options.input);
const outputDir = resolve(options.output);
await fs.mkdir(outputDir, { recursive: true });

const entries = await fs.readdir(inputDir, { withFileTypes: true });
const collator = new Intl.Collator("zh-CN");
const imageFiles = entries
  .filter((entry) => entry.isFile() && extname(entry.name).toLowerCase() === ".png")
  .map((entry) => ({
    name: entry.name,
    fullPath: join(inputDir, entry.name),
  }))
  .sort((left, right) => collator.compare(left.name, right.name));

if (imageFiles.length === 0) {
  throw new Error(`输入目录中没有 PNG 文件: ${inputDir}`);
}

const requireFromFrontend = createRequire(resolve(repoRoot, "frontend/package.json"));
const { chromium } = requireFromFrontend("playwright");
const browserCandidates = [
  "C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe",
  "C:\\Program Files (x86)\\Microsoft\\Edge\\Application\\msedge.exe",
];
const executablePath = browserCandidates.find((candidate) => existsSync(candidate));
const browser = await chromium.launch({
  headless: true,
  ...(executablePath ? { executablePath } : {}),
});
const page = await browser.newPage({ viewport: { width: 1280, height: 720 }, deviceScaleFactor: 1 });
await page.setContent("<!doctype html><html><head><meta charset='utf-8'></head><body></body></html>");

const manifest = [];
const sampleSource = getComparisonSample(imageFiles);
let sampleOriginalDataUrl = null;
let sampleEnhancedDataUrl = null;

for (const imageFile of imageFiles) {
  const sourceBuffer = await fs.readFile(imageFile.fullPath);
  const originalDataUrl = bufferToDataUrl(sourceBuffer);
  const enhanced = await enhanceImage(page, originalDataUrl, options.scale, options.sharpen);
  const outputName = `${imageFile.name.slice(0, -4)}-高清增强.png`;
  const outputPath = join(outputDir, outputName);
  const outputBuffer = applyPngDpi(dataUrlToBuffer(enhanced.dataUrl), options.dpi);
  await fs.writeFile(outputPath, outputBuffer);

  manifest.push({
    name: imageFile.name,
    sourcePath: imageFile.fullPath,
    outputPath,
    originalWidth: enhanced.width / options.scale,
    originalHeight: enhanced.height / options.scale,
    enhancedWidth: enhanced.width,
    enhancedHeight: enhanced.height,
    scale: options.scale,
    sharpenStrength: options.sharpen,
    dpi: options.dpi,
  });

  if (imageFile.name === sampleSource.name) {
    sampleOriginalDataUrl = originalDataUrl;
    sampleEnhancedDataUrl = enhanced.dataUrl;
  }
}

if (sampleOriginalDataUrl && sampleEnhancedDataUrl) {
  const comparisonDataUrl = await createComparison(page, sampleOriginalDataUrl, sampleEnhancedDataUrl, options.scale);
  const comparisonBuffer = applyPngDpi(dataUrlToBuffer(comparisonDataUrl), options.dpi);
  await fs.writeFile(join(outputDir, "comparison-center-crop.png"), comparisonBuffer);
}

await browser.close();
await fs.writeFile(join(outputDir, "manifest.json"), `${JSON.stringify(manifest, null, 2)}\n`, "utf8");

console.log(`输入目录: ${inputDir}`);
console.log(`输出目录: ${outputDir}`);
console.log(`处理文件数: ${manifest.length}`);
console.log(`样例对比图: ${join(outputDir, "comparison-center-crop.png")}`);
