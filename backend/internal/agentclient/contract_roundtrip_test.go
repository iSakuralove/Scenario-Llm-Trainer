package agentclient

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"
)

// TestGoldenPythonPayloadSurvivesStrictDecode 守的是 Python 契约与 Go 结构体的字段一致性。
//
// client.go 对最终 result 事件用 DisallowUnknownFields()——这是"Go 不信任 Python"
// 原则的一部分，不能放松。代价是 Python 侧给契约加任何字段，Go 漏声明就会整轮
// 以 agent_invalid_response 失败，而且失败发生在推理摘要和正文都流完之后
// （那两条走宽松的 json.Unmarshal），用户看到的是"明明响应成功却被判无效"。
//
// 单侧单测抓不到这个：Go 测试自己构造 TurnResult，Python 测试不碰 Go。
// 只有拿真实 Python 产出的 payload 过一遍严格解码才能守住。
//
// testdata/turn_result_golden.json 由 agent 侧真实主链产出，更新契约时同步重生成。
func TestGoldenPythonPayloadSurvivesStrictDecode(t *testing.T) {
	payload, err := os.ReadFile("testdata/turn_result_golden.json")
	if err != nil {
		t.Fatalf("read golden payload: %v", err)
	}

	var result TurnResult
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		t.Fatalf("strict decode of a real Python payload failed — Go 结构体缺字段: %v", err)
	}

	if result.TurnAnalysis.PublicSummary == "" {
		t.Error("public_summary 未被解码：流式推理摘要的来源字段丢失")
	}
	if !result.TurnAnalysis.IsStuck {
		t.Error("is_stuck 未被解码")
	}
	var stall bool
	for _, proposal := range result.Proposals {
		if proposal.Kind == "release_evidence_on_stall" {
			stall = true
		}
	}
	if !stall {
		t.Error("golden payload 应包含一条 release_evidence_on_stall 提议")
	}
}

// TestStrictDecodeRejectsUnknownContractField 明确记录上面那条 golden 测试守的是什么机制。
// 这里刻意不放松 DisallowUnknownFields——Go 拒绝未知字段是"不信任 Python"的一部分。
func TestStrictDecodeRejectsUnknownContractField(t *testing.T) {
	payload, err := os.ReadFile("testdata/turn_result_golden.json")
	if err != nil {
		t.Fatalf("read golden payload: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(payload, &raw); err != nil {
		t.Fatalf("unmarshal golden payload: %v", err)
	}
	analysis, ok := raw["turn_analysis"].(map[string]any)
	if !ok {
		t.Fatal("golden payload is missing turn_analysis")
	}
	analysis["field_python_added_but_go_forgot"] = "x"
	mutated, err := json.Marshal(raw)
	if err != nil {
		t.Fatalf("marshal mutated payload: %v", err)
	}

	decoder := json.NewDecoder(bytes.NewReader(mutated))
	decoder.DisallowUnknownFields()
	var result TurnResult
	if err := decoder.Decode(&result); err == nil {
		t.Fatal("strict decode accepted an unknown contract field; 契约漂移将不再被发现")
	}
}
