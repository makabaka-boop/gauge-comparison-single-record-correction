package solver

import (
	"errors"
	"fmt"
)

// ErrUnknownRecord 表示纠错查询指定的疑似记录 id 不存在。
var ErrUnknownRecord = errors.New("solver: unknown record id")

// 纠错查询结论的类别。
const (
	// StatusOtherConflict 移除疑似记录后其余记录仍矛盾：单改此条无效。
	StatusOtherConflict = "otherConflict"
	// StatusUndetermined 其余记录一致但疑似记录两端不连通：差值无法唯一推定。
	StatusUndetermined = "undetermined"
	// StatusSuggested 两端连通且唯一应填 delta 在既有取值范围内。
	StatusSuggested = "suggested"
	// StatusOutOfRange 两端连通但唯一应填 delta 超出既有取值范围：
	// 不提供非法修正建议。
	StatusOutOfRange = "outOfRange"
)

// Correct 纠错查询：暂时移除 id 为 suspectID 的记录，按原 id 顺序复核其余
// 约束，判断只改这一条记录能否修复整张校准网。
//
//   - 其余记录仍矛盾：返回按 id 顺序最先出现的另一条冲突及闭环证据
//     （StatusOtherConflict），单改此条无效；
//   - 其余记录一致但疑似记录两端不连通：差值无法唯一推定
//     （StatusUndetermined）；
//   - 两端连通：用势能差 value[to]-value[from] 求唯一应填 delta，附森林
//     路径、逐步符号与和原值的差额（StatusSuggested）；应填值超出
//     [-maxDelta, maxDelta] 时为 StatusOutOfRange，不给非法修正建议。
//
// 查询不修改 records，也不影响 Solve 的核验结果（各自内部复制）。
func Correct(standards []string, records []Record, suspectID string, maxDelta int64) (Correction, error) {
	var suspect Record
	found := false
	rest := make([]Record, 0, len(records))
	for i := range records {
		if records[i].ID == suspectID {
			if !found {
				suspect = records[i] // 复制，后续不触碰原切片元素
				found = true
			}
			continue
		}
		rest = append(rest, records[i])
	}
	if !found {
		return Correction{}, fmt.Errorf("%w: %q", ErrUnknownRecord, suspectID)
	}

	c := Correction{Suspect: suspect}
	p := propagate(standards, rest)

	// 其余记录仍矛盾：首条失效记录必然不是疑似记录本身。
	if !p.result.Consistent {
		c.Status = StatusOtherConflict
		c.OtherConflict = p.result.Conflict
		c.Message = fmt.Sprintf(
			"移除记录 %s 后其余记录仍矛盾（按 id 顺序首条失效记录为 %s），只改此条记录无法修复校准网",
			suspectID, p.result.Conflict.Record.ID)
		return c, nil
	}

	a, b := p.index[suspect.From], p.index[suspect.To]
	if p.d.find(a) != p.d.find(b) {
		c.Status = StatusUndetermined
		c.Message = fmt.Sprintf(
			"其余记录一致，但移除记录 %s 后 %s 与 %s 不连通，差值无法由其余记录唯一推定",
			suspectID, suspect.From, suspect.To)
		return c, nil
	}

	// 两端连通（自比较时 a == b 自然连通，隐含差为 0）：
	// value[to]-value[from] = pot[b]-pot[a] 唯一确定应填 delta。
	implied := p.d.pot[b] - p.d.pot[a]
	fix := &Fix{
		Delta: implied,
		Diff:  implied - suspect.Delta,
		Path:  forestPath(a, b, p.adj, p.ordered, p.standards),
		Legal: implied <= maxDelta && implied >= -maxDelta,
	}
	c.Fix = fix
	switch {
	case !fix.Legal:
		c.Status = StatusOutOfRange
		c.Message = fmt.Sprintf(
			"唯一应填 delta 为 %d，超出既有取值范围 [-%d, %d]，不提供非法修正建议",
			implied, maxDelta, maxDelta)
	case fix.Diff == 0:
		c.Status = StatusSuggested
		c.Message = fmt.Sprintf("记录 %s 的差值与其余记录一致，无需修改", suspectID)
	default:
		c.Status = StatusSuggested
		c.Message = fmt.Sprintf("建议将记录 %s 的 delta 改为 %d（与原值相差 %+d）",
			suspectID, implied, fix.Diff)
	}
	return c, nil
}
