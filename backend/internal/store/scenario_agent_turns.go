package store

import (
	"context"
	"sort"

	"situational-teaching/backend/internal/domain"
)

// ListScenarioAgentTurns 返回一条会话的结构化 Agent 轮次，按提交 revision 排序。
// 评测和审计只读这些已原子提交的快照，不从公开消息重新猜测状态。
func (s *MemoryStore) ListScenarioAgentTurns(sessionID string) []domain.ScenarioAgentTurnRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()

	items := make([]domain.ScenarioAgentTurnRecord, 0)
	for _, record := range s.ScenarioAgentTurns {
		if record.SessionID == sessionID {
			items = append(items, cloneScenarioAgentTurnRecord(record))
		}
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].CommittedRevision != items[j].CommittedRevision {
			return items[i].CommittedRevision < items[j].CommittedRevision
		}
		return items[i].CreatedAt.Before(items[j].CreatedAt)
	})
	return items
}

func (s *PostgresStore) ListScenarioAgentTurns(sessionID string) []domain.ScenarioAgentTurnRecord {
	rows, err := s.pool.Query(context.Background(), `
		SELECT result_snapshot
		FROM scenario_agent_turns
		WHERE session_id = $1
		ORDER BY committed_revision ASC, created_at ASC
	`, sessionID)
	if err != nil {
		return []domain.ScenarioAgentTurnRecord{}
	}
	defer rows.Close()

	items := make([]domain.ScenarioAgentTurnRecord, 0)
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			continue
		}
		var record domain.ScenarioAgentTurnRecord
		if err := unmarshal(raw, &record); err == nil {
			items = append(items, record)
		}
	}
	return items
}
