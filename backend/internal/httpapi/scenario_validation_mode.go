package httpapi

import (
	"log"
	"os"
	"strings"

	"situational-teaching/backend/internal/domain"
)

// scenarioValidationMode 决定排查工坊 Go 侧业务闸门（proposal 审批、
// 回复防剧透 guard）的执行方式。公开 trace 是可丢弃旁路，见
// scenarioPublicTraceValidationMode，不再以单条过程事件阻断整轮：
//
//   - strict：默认。业务闸门任一校验失败即拒绝整轮（502 + 错误码），与历史行为一致。
//   - log：影子模式。校验失败只记审计与日志，不拦截整轮。用于观察
//     「Python Guard 放行、Go 校验更严」的真实分歧面，避免演示现场整轮报废。
//   - off：跳过全部闸门，直接信任 Python 侧结果。仅限本地联调。
//
// 注意：公开 trace 只记录并过滤，不论 strict/log；revision 校验、request_id 幂等、CommitScenarioAgentTurn 原子提交
// 不属于这三组校验器，任何模式下都必须原样执行——它们是会话一致性的根基，
// 见 docs/wayfinder/003-go-state-audit-seam.md。
type scenarioValidationMode string

const (
	scenarioValidationStrict scenarioValidationMode = "strict"
	scenarioValidationLog    scenarioValidationMode = "log"
	scenarioValidationOff    scenarioValidationMode = "off"
)

// scenarioValidationModeFromEnv 解析 SCENARIO_VALIDATION_MODE。
// 空值与未知值一律回退 strict——宁紧勿松，避免手滑把生产校验关掉。
func scenarioValidationModeFromEnv() scenarioValidationMode {
	raw := strings.ToLower(strings.TrimSpace(getenvValue("SCENARIO_VALIDATION_MODE")))
	switch scenarioValidationMode(raw) {
	case "", scenarioValidationStrict:
		return scenarioValidationStrict
	case scenarioValidationLog:
		return scenarioValidationLog
	case scenarioValidationOff:
		return scenarioValidationOff
	default:
		log.Printf("invalid SCENARIO_VALIDATION_MODE %q, falling back to strict", raw)
		return scenarioValidationStrict
	}
}

// scenarioPublicTraceValidationModeFromEnv 解析过程事件专用过滤档位。
// V2 迁移窗口默认使用 log，让不兼容的旧过程事件被记录并丢弃，而不是把已经
// 可以展示的正文一起截断。strict 仅保留给审计/兼容配置，不会恢复“单条 trace
// 阻断正文”的行为；过程事件始终是可丢弃旁路。
func scenarioPublicTraceValidationModeFromEnv() scenarioValidationMode {
	raw := strings.ToLower(strings.TrimSpace(getenvValue("SCENARIO_PUBLIC_TRACE_VALIDATION_MODE")))
	if raw == "" {
		return scenarioValidationLog
	}
	switch scenarioValidationMode(raw) {
	case scenarioValidationStrict, scenarioValidationLog, scenarioValidationOff:
		return scenarioValidationMode(raw)
	default:
		log.Printf("invalid SCENARIO_PUBLIC_TRACE_VALIDATION_MODE %q, falling back to log", raw)
		return scenarioValidationLog
	}
}

// getenvValue 抽出 os.Getenv，便于测试注入。
var getenvValue = os.Getenv

// bypassAudit 描述一次被放宽的校验（log 模式记录用）。
type bypassAudit struct {
	Validator string `json:"validator"`
	RequestID string `json:"request_id"`
	Violation string `json:"violation"`
	Mode      string `json:"mode"`
}

// recordScenarioValidationBypass 在 log 模式下记录一次校验分歧：
// 服务端审计事件 + stderr 日志双通道，off 模式静默跳过。
func (s *Server) recordScenarioValidationBypass(validator, requestID, violation string) {
	entry := bypassAudit{
		Validator: validator,
		RequestID: requestID,
		Violation: violation,
		Mode:      string(s.scenarioValidationMode),
	}
	log.Printf("[scenario-validation] mode=%s validator=%s request_id=%s violation=%s",
		entry.Mode, entry.Validator, entry.RequestID, entry.Violation)
	s.store.RecordAuditEvent(domain.AuditEvent{
		Action:       "scenario.validation_bypassed",
		ResourceType: "scenario_session",
		ResourceID:   entry.RequestID,
		Metadata: map[string]string{
			"validator": entry.Validator,
			"violation": truncateViolation(entry.Violation),
			"mode":      entry.Mode,
		},
	})
}

// truncateViolation 防止违规详情（可能引用题库内容）把审计行撑爆。
func truncateViolation(violation string) string {
	const maxRunes = 200
	runes := []rune(violation)
	if len(runes) <= maxRunes {
		return violation
	}
	return string(runes[:maxRunes]) + "..."
}
