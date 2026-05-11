package ai

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"text/template"
)

const (
	PromptRenderEngineGoTemplate = "go_template"
	PromptRenderEngineJinja2     = "jinja2"
)

//go:embed prompts/*.tmpl
var promptFS embed.FS

var promptTemplates = template.Must(template.ParseFS(promptFS, "prompts/*.tmpl"))
var promptOverrides sync.Map
var promptOverrideEngines sync.Map

func renderPrompt(name string, data interface{}) (string, error) {
	if value, ok := promptOverrides.Load(name); ok {
		engine := promptRenderEngine(name, PromptRenderEngineGoTemplate)
		return renderPromptText(name, engine, value.(string), data)
	}
	var buf bytes.Buffer
	if err := promptTemplates.ExecuteTemplate(&buf, name+".tmpl", data); err != nil {
		return "", fmt.Errorf("render prompt %s: %w", name, err)
	}
	return strings.TrimSpace(buf.String()), nil
}

func SetPromptOverride(name, renderEngine, content string) error {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		promptOverrides.Delete(name)
		promptOverrideEngines.Delete(name)
		return nil
	}
	engine := normalizePromptRenderEngine(renderEngine)
	if err := validatePromptText(name, engine, trimmed); err != nil {
		return err
	}
	if err := ValidateManagedPromptContent(name, trimmed); err != nil {
		return err
	}
	promptOverrides.Store(name, trimmed)
	promptOverrideEngines.Store(name, engine)
	return nil
}

func ClearPromptOverride(name string) {
	promptOverrides.Delete(name)
	promptOverrideEngines.Delete(name)
}

func normalizePromptRenderEngine(engine string) string {
	switch strings.TrimSpace(strings.ToLower(engine)) {
	case PromptRenderEngineJinja2:
		return PromptRenderEngineJinja2
	default:
		return PromptRenderEngineGoTemplate
	}
}

func promptRenderEngine(name, fallback string) string {
	if value, ok := promptOverrideEngines.Load(name); ok {
		return normalizePromptRenderEngine(value.(string))
	}
	return normalizePromptRenderEngine(fallback)
}

func DefaultPromptContent(name string) string {
	content, err := promptFS.ReadFile("prompts/" + strings.TrimSpace(name) + ".tmpl")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(content))
}

func ValidateManagedPromptContent(name, text string) error {
	if strings.TrimSpace(name) != "scenario_generate" {
		return nil
	}
	trimmed := strings.TrimSpace(text)
	if len([]rune(trimmed)) < 200 {
		return fmt.Errorf("情景题生成 Prompt 必须保留结构化 JSON 骨架，当前内容过短")
	}
	required := []string{
		`"title"`,
		`"description"`,
		`"content"`,
		`"root_cause"`,
		`"root_cause_keywords"`,
		`"architecture_diagram_spec"`,
		`"reveal_strategy"`,
		`"surface_clues"`,
		`"deep_clues"`,
		`"distractors"`,
	}
	missing := []string{}
	for _, token := range required {
		if !strings.Contains(trimmed, token) {
			missing = append(missing, token)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("情景题生成 Prompt 必须保留结构化 JSON 骨架，缺少字段：%s", strings.Join(missing, ", "))
	}
	return nil
}

func validatePromptText(name, engine, text string) error {
	switch normalizePromptRenderEngine(engine) {
	case PromptRenderEngineJinja2:
		return jinja2TemplateValidate(name, text)
	default:
		if _, err := template.New(name).Parse(text); err != nil {
			return fmt.Errorf("parse prompt %s: %w", name, err)
		}
		return nil
	}
}

func renderPromptText(name, engine, text string, data interface{}) (string, error) {
	switch normalizePromptRenderEngine(engine) {
	case PromptRenderEngineJinja2:
		return jinja2TemplateRender(name, text, data)
	default:
		tmpl, err := template.New(name).Parse(text)
		if err != nil {
			return "", fmt.Errorf("render prompt %s: %w", name, err)
		}
		var buf bytes.Buffer
		if err := tmpl.Execute(&buf, data); err != nil {
			return "", fmt.Errorf("render prompt %s: %w", name, err)
		}
		return strings.TrimSpace(buf.String()), nil
	}
}

func jinja2TemplateValidate(name, text string) error {
	return runJinja2Command(name, text, nil, "validate")
}

func jinja2TemplateRender(name, text string, data interface{}) (string, error) {
	payload, err := json.Marshal(data)
	if err != nil {
		return "", fmt.Errorf("render prompt %s: %w", name, err)
	}
	var stdout bytes.Buffer
	if err := runJinja2CommandWithOutput(name, text, payload, "render", &stdout); err != nil {
		return "", err
	}
	return strings.TrimSpace(stdout.String()), nil
}

func runJinja2Command(name, text string, payload []byte, mode string) error {
	return runJinja2CommandWithOutput(name, text, payload, mode, nil)
}

func runJinja2CommandWithOutput(name, text string, payload []byte, mode string, stdout *bytes.Buffer) error {
	script := strings.Join([]string{
		"import json, sys",
		"from jinja2 import Environment, StrictUndefined",
		"template = sys.stdin.read()",
		"env = Environment(undefined=StrictUndefined, autoescape=False, trim_blocks=False, lstrip_blocks=False)",
		"compiled = env.from_string(template)",
		"payload = json.loads(sys.argv[2]) if len(sys.argv) > 2 and sys.argv[2] else {}",
		"if sys.argv[1] == 'render':",
		"    sys.stdout.write(compiled.render(**payload).strip())",
	}, "\n")
	args := []string{"-c", script, mode}
	if payload == nil {
		args = append(args, "")
	} else {
		args = append(args, string(payload))
	}
	cmd := exec.Command("python", args...)
	cmd.Stdin = strings.NewReader(text)
	if stdout != nil {
		cmd.Stdout = stdout
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		return fmt.Errorf("render prompt %s: %s", name, message)
	}
	return nil
}
