import { dirname, resolve } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import { createRequire } from "node:module";
import { existsSync } from "node:fs";

const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const requireFromFrontend = createRequire(resolve(repoRoot, "frontend/package.json"));
const { chromium: frontendChromium } = requireFromFrontend("playwright");
const htmlPath = resolve(repoRoot, "docs/figures/figure-2-1-system-architecture.html");
const outputPath = resolve(repoRoot, "docs/figures/figure-2-1-system-architecture.png");

const browserCandidates = [
  "C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe",
  "C:\\Program Files (x86)\\Microsoft\\Edge\\Application\\msedge.exe",
];
const executablePath = browserCandidates.find((candidate) => existsSync(candidate));
const browser = await frontendChromium.launch({
  headless: true,
  ...(executablePath ? { executablePath } : {}),
});
const page = await browser.newPage({ viewport: { width: 2400, height: 1350 }, deviceScaleFactor: 1 });

await page.goto(pathToFileURL(htmlPath).href, { waitUntil: "networkidle" });
await page.screenshot({ path: outputPath, type: "png", fullPage: false });
await browser.close();

console.log(`已导出：${outputPath}`);
