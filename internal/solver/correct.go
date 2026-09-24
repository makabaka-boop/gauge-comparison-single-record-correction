package solver

import "fmt"

// Correct 评估“只改 suspectID 这一条记录”能否修复整张校准网：
// 暂时移除该记录，按 id 的 UTF-8 字节序复核其余约束，再据连通性给出结论。
//
//   - 其余记录仍矛盾：返回最先失效的另一条记录及其闭环证据（单改此条无效）；
//   - 其余记录一致但疑似记录两端不连通：差值无法唯一推定；
//   - 其余记录一致且两端连通：唯一应填 delta 即势能差 pot[to]-pot[from]，
//     附森林路径与逐步符号；超出 [-MaxDelta, MaxDelta] 时不给出修正建议。
//
// 本函数为纯查询：不修改 records，也不影响 Solve 的核验结果。
// 第二个返回值报告 suspectID 是否对应已有记录。
func Correct(standards []string, records []Record, suspectID string) (Correction, bool) {
	var suspect Record
	found := false
	rest := make([]Record, 0, len(records))
	for _, r := range records {
		if !found && r.ID == suspectID {
			suspect = r
			found = true
			continue
		}
		rest = append(rest, r)
	}
	if !found {
		return Correction{}, false
	}

	nw := newNetwork(standards, rest)
	if c := nw.absorb(); c != nil {
		return Correction{
			Suspect: suspect,
			Status:  CorrectionStillConflicting,
			Note: fmt.Sprintf(
				"移除 %s 后其余记录仍矛盾：%s 最先失效（闭环和 %d），仅修改 %s 无法修复整网",
				suspect.ID, c.Record.ID, c.Loop.Sum, suspect.ID),
			Conflict: c,
		}, true
	}

	a, b := nw.index[suspect.From], nw.index[suspect.To]
	if !nw.connected(a, b) {
		return Correction{
			Suspect: suspect,
			Status:  CorrectionUnderdetermined,
			Note: fmt.Sprintf(
				"移除 %s 后其余记录一致，但 %s 与 %s 分属不同连通分量，差值无法唯一推定",
				suspect.ID, suspect.From, suspect.To),
		}, true
	}

	implied := nw.diff(a, b)
	path := forestPath(a, b, nw.adj, nw.ordered, nw.standards)
	// 同一森林、同一势能：path.Total 与 implied 必然相等。

	if implied > MaxDelta || implied < -MaxDelta {
		return Correction{
			Suspect: suspect,
			Status:  CorrectionOutOfRange,
			Note: fmt.Sprintf(
				"其余记录隐含的唯一差值 %d 超出既有取值范围 [-%d, %d]，不提供非法修正建议",
				implied, MaxDelta, MaxDelta),
			ImpliedDelta: &implied,
			Path:         &path,
		}, true
	}

	change := implied - suspect.Delta
	note := fmt.Sprintf(
		"其余记录一致：唯一应填差值 %d（原值 %d，差额 %+d），单改此条即可修复整网",
		implied, suspect.Delta, change)
	if change == 0 {
		note = fmt.Sprintf("疑似记录 %s 与其余记录一致，无需修改", suspect.ID)
	}
	return Correction{
		Suspect:        suspect,
		Status:         CorrectionFixed,
		Note:           note,
		ImpliedDelta:   &implied,
		SuggestedDelta: &implied,
		DeltaChange:    &change,
		Path:           &path,
	}, true
}
