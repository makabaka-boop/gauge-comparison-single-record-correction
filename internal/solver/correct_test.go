package solver

import (
	"errors"
	"fmt"
	"math/rand"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// 反向：其余记录中的森林路径需要逆着记录方向行走，符号必须取反。
func TestCorrectReversePath(t *testing.T) {
	standards := []string{"A", "B", "C"}
	records := []Record{
		{ID: "r1", From: "B", To: "A", Delta: -2}, // A-B=-2，即 B=A+2
		{ID: "r2", From: "C", To: "B", Delta: 5},  // B-C=5，即 C=B-5
		{ID: "r3", From: "A", To: "C", Delta: 0},  // 疑似抄错；真实 C-A=-3
	}
	c, err := Correct(standards, records, "r3", maxDeltaTest)
	if err != nil {
		t.Fatal(err)
	}
	if c.Status != StatusSuggested {
		t.Fatalf("status = %s, want %s", c.Status, StatusSuggested)
	}
	// B=A+2，C=B-5=A-3，故 C-A=-3；与原值 0 相差 -3。
	if c.Fix.Delta != -3 || c.Fix.Diff != -3 {
		t.Fatalf("fix = %+v, want delta/diff -3/-3", c.Fix)
	}
	// 路径 A->B->C：r1、r2 均需反向行走，符号取反。
	wantSteps := []Step{
		{RecordID: "r1", From: "A", To: "B", Direction: "reverse", SignedDelta: 2},
		{RecordID: "r2", From: "B", To: "C", Direction: "reverse", SignedDelta: -5},
	}
	if !reflect.DeepEqual(c.Fix.Path.Steps, wantSteps) {
		t.Fatalf("steps = %+v, want %+v", c.Fix.Path.Steps, wantSteps)
	}
	if !reflect.DeepEqual(c.Fix.Path.RunningTotal, []int64{2, -3}) {
		t.Fatalf("running total = %v, want [2 -3]", c.Fix.Path.RunningTotal)
	}
	verifyFix(t, records, c, maxDeltaTest)
}

// 平行：同一对标准件的两条记录不一致时，另一条即隐含值。
func TestCorrectParallelRecords(t *testing.T) {
	standards := []string{"A", "B"}
	records := []Record{
		{ID: "p1", From: "A", To: "B", Delta: 10},
		{ID: "p2", From: "A", To: "B", Delta: 11}, // 疑似
	}
	c, err := Correct(standards, records, "p2", maxDeltaTest)
	if err != nil {
		t.Fatal(err)
	}
	if c.Status != StatusSuggested || c.Fix.Delta != 10 || c.Fix.Diff != -1 {
		t.Fatalf("correction = %+v, want suggest 10 diff -1", c)
	}
	if len(c.Fix.Path.Steps) != 1 || c.Fix.Path.Steps[0].RecordID != "p1" {
		t.Fatalf("path = %+v, want single p1 step", c.Fix.Path)
	}
	verifyFix(t, records, c, maxDeltaTest)

	// 对称地查 p1：建议改为 11。
	c2, err := Correct(standards, records, "p1", maxDeltaTest)
	if err != nil {
		t.Fatal(err)
	}
	if c2.Status != StatusSuggested || c2.Fix.Delta != 11 || c2.Fix.Diff != 1 {
		t.Fatalf("correction for p1 = %+v, want suggest 11 diff 1", c2)
	}
	verifyFix(t, records, c2, maxDeltaTest)
}

// 自比较：value[x]-value[x] 恒为 0，抄错的自比较记录应建议改 0；
// 而查其他记录时，这条坏自比较就是“另一条冲突”。
func TestCorrectSelfComparison(t *testing.T) {
	standards := []string{"A", "B"}
	records := []Record{
		{ID: "g1", From: "A", To: "B", Delta: 4},
		{ID: "s1", From: "A", To: "A", Delta: 3}, // 自比较抄错
	}

	c, err := Correct(standards, records, "s1", maxDeltaTest)
	if err != nil {
		t.Fatal(err)
	}
	if c.Status != StatusSuggested || c.Fix.Delta != 0 || c.Fix.Diff != -3 {
		t.Fatalf("correction = %+v, want suggest 0 diff -3", c)
	}
	if len(c.Fix.Path.Steps) != 0 || c.Fix.Path.Total != 0 {
		t.Fatalf("self-comparison path should be empty, got %+v", c.Fix.Path)
	}
	verifyFix(t, records, c, maxDeltaTest)

	// 查 g1：其余记录里 s1（A-A=3）立即矛盾，单改 g1 无效。
	c2, err := Correct(standards, records, "g1", maxDeltaTest)
	if err != nil {
		t.Fatal(err)
	}
	if c2.Status != StatusOtherConflict || c2.OtherConflict.Record.ID != "s1" {
		t.Fatalf("correction = %+v, want otherConflict at s1", c2)
	}
	if c2.OtherConflict.Loop.Sum != -3 {
		t.Fatalf("loop sum = %d, want -3", c2.OtherConflict.Loop.Sum)
	}
}

// 桥边：疑似记录是连接两个分量的唯一边，移除后两端不连通，
// 差值可任取，无法唯一推定。
func TestCorrectBridgeEdge(t *testing.T) {
	standards := []string{"A", "B", "C", "D"}
	records := []Record{
		{ID: "b1", From: "A", To: "B", Delta: 1},
		{ID: "b2", From: "C", To: "D", Delta: 2},
		{ID: "br", From: "B", To: "C", Delta: 5}, // 唯一桥边
	}
	c, err := Correct(standards, records, "br", maxDeltaTest)
	if err != nil {
		t.Fatal(err)
	}
	if c.Status != StatusUndetermined {
		t.Fatalf("status = %s, want %s", c.Status, StatusUndetermined)
	}
	if c.Fix != nil || c.OtherConflict != nil {
		t.Fatalf("undetermined should carry no fix/conflict: %+v", c)
	}
	if !strings.Contains(c.Message, "不连通") || !strings.Contains(c.Message, "无法") {
		t.Fatalf("message should explain undetermined: %q", c.Message)
	}
	// 独立图遍历确认 B、C 在其余记录中确实不连通。
	if _, reachable := bfsPathSum(restOf(records, "br"), "B", "C"); reachable {
		t.Fatal("B and C should be disconnected without the bridge")
	}
}

// 多处矛盾：移除疑似记录后其余记录仍矛盾，返回按 id 顺序最先出现的
// 另一条冲突及闭环证据，并明确说明单改此条无效。
func TestCorrectMultipleConflicts(t *testing.T) {
	standards := []string{"A", "B", "C", "D", "E"}
	records := []Record{
		{ID: "m1", From: "A", To: "B", Delta: 1},
		{ID: "m2", From: "B", To: "C", Delta: 1},
		{ID: "m3-bad", From: "A", To: "C", Delta: 9}, // 疑似抄错（隐含 2）
		{ID: "m4", From: "D", To: "E", Delta: 1},
		{ID: "m5-bad", From: "E", To: "D", Delta: 7},  // 另一处矛盾（先出现）
		{ID: "m6-bad", From: "A", To: "B", Delta: 99}, // 还有一处（后出现）
	}
	c, err := Correct(standards, records, "m3-bad", maxDeltaTest)
	if err != nil {
		t.Fatal(err)
	}
	if c.Status != StatusOtherConflict {
		t.Fatalf("status = %s, want %s", c.Status, StatusOtherConflict)
	}
	oc := c.OtherConflict
	if oc == nil || oc.Record.ID == "m3-bad" {
		t.Fatalf("other conflict must be another record: %+v", oc)
	}
	// 字节序 m4 < m5-bad < m6-bad：首条失效的是 m5-bad，而非 m6-bad。
	if oc.Record.ID != "m5-bad" {
		t.Fatalf("first other conflict = %s, want m5-bad", oc.Record.ID)
	}
	// m4 隐含 D-E=1，即 E-D=-1；m5-bad 声称 E-D=7。
	if oc.ImpliedDelta != -1 || oc.Mismatch != 8 {
		t.Fatalf("implied=%d mismatch=%d, want -1/8", oc.ImpliedDelta, oc.Mismatch)
	}
	// 闭环证据：E->D 单步反向走 m4，环和 -1-7=-8。
	if oc.Path.Total != -1 || oc.Loop.Sum != -8 {
		t.Fatalf("path total %d, loop sum %d, want -1/-8", oc.Path.Total, oc.Loop.Sum)
	}
	if !strings.Contains(c.Message, "只改此条记录无法修复") {
		t.Fatalf("message should state single-fix useless: %q", c.Message)
	}
	if c.Fix != nil {
		t.Fatalf("otherConflict should carry no fix: %+v", c.Fix)
	}
	// 用与生产实现独立的朴素 DSU 复核整条闭环证据。
	verifyConflict(t, genNetwork{standards: standards}, restOf(records, "m3-bad"), oc.Record, oc)

	// 对称地查 m5-bad：首条失效变为 m3-bad。
	c2, err := Correct(standards, records, "m5-bad", maxDeltaTest)
	if err != nil {
		t.Fatal(err)
	}
	if c2.Status != StatusOtherConflict || c2.OtherConflict.Record.ID != "m3-bad" {
		t.Fatalf("correction for m5-bad = %+v, want otherConflict at m3-bad", c2)
	}
	verifyConflict(t, genNetwork{standards: standards}, restOf(records, "m5-bad"), c2.OtherConflict.Record, c2.OtherConflict)
}

// 超出既有 delta 取值范围：唯一应填值非法时只给证据，不给修正建议。
func TestCorrectOutOfRange(t *testing.T) {
	standards := []string{"A", "B", "C"}
	records := []Record{
		{ID: "o1", From: "A", To: "B", Delta: maxDeltaTest},
		{ID: "o2", From: "B", To: "C", Delta: maxDeltaTest},
		{ID: "o3", From: "A", To: "C", Delta: 0}, // 疑似；隐含 2*10^9 超界
	}
	c, err := Correct(standards, records, "o3", maxDeltaTest)
	if err != nil {
		t.Fatal(err)
	}
	if c.Status != StatusOutOfRange {
		t.Fatalf("status = %s, want %s", c.Status, StatusOutOfRange)
	}
	if c.Fix == nil || c.Fix.Delta != 2*maxDeltaTest || c.Fix.Legal {
		t.Fatalf("fix = %+v, want delta %d marked illegal", c.Fix, 2*maxDeltaTest)
	}
	if c.Fix.Diff != 2*maxDeltaTest {
		t.Fatalf("diff = %d, want %d", c.Fix.Diff, 2*maxDeltaTest)
	}
	if !strings.Contains(c.Message, "不提供非法修正建议") {
		t.Fatalf("message should refuse illegal fix: %q", c.Message)
	}
	// 证据本身仍需完整、可独立复核。
	verifyFix(t, records, c, maxDeltaTest)
}

// 网络本就一致：疑似记录与其余记录吻合，建议值等于原值（差额 0）。
func TestCorrectNoChangeNeeded(t *testing.T) {
	standards := []string{"A", "B", "C"}
	records := []Record{
		{ID: "r1", From: "A", To: "B", Delta: 2},
		{ID: "r2", From: "B", To: "C", Delta: 3},
		{ID: "r3", From: "A", To: "C", Delta: 5},
	}
	c, err := Correct(standards, records, "r3", maxDeltaTest)
	if err != nil {
		t.Fatal(err)
	}
	if c.Status != StatusSuggested || c.Fix.Delta != 5 || c.Fix.Diff != 0 {
		t.Fatalf("correction = %+v, want unchanged suggestion 5", c)
	}
	if !strings.Contains(c.Message, "无需修改") {
		t.Fatalf("message = %q, want 无需修改", c.Message)
	}
	verifyFix(t, records, c, maxDeltaTest)
}

func TestCorrectUnknownSuspect(t *testing.T) {
	records := []Record{{ID: "x", From: "A", To: "B", Delta: 1}}
	_, err := Correct([]string{"A", "B"}, records, "nope", maxDeltaTest)
	if !errors.Is(err, ErrUnknownRecord) {
		t.Fatalf("err = %v, want ErrUnknownRecord", err)
	}
}

// 纠错查询不修改原记录，也不改变原核验结果。
func TestCorrectDoesNotMutateInputs(t *testing.T) {
	standards := []string{"A", "B", "C"}
	records := []Record{
		{ID: "r1", From: "A", To: "B", Delta: 2},
		{ID: "r2", From: "B", To: "C", Delta: 3},
		{ID: "r3", From: "A", To: "C", Delta: 4}, // 矛盾
	}
	origRecords := append([]Record{}, records...)
	origStandards := append([]string{}, standards...)
	before := Solve(standards, records)

	if _, err := Correct(standards, records, "r3", maxDeltaTest); err != nil {
		t.Fatal(err)
	}
	if _, err := Correct(standards, records, "r1", maxDeltaTest); err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(records, origRecords) || !reflect.DeepEqual(standards, origStandards) {
		t.Fatal("Correct mutated its inputs")
	}
	if after := Solve(standards, records); !reflect.DeepEqual(before, after) {
		t.Fatalf("Solve result changed: before %+v after %+v", before, after)
	}
}

// 随机网络 + 随机疑似记录：与测试侧独立实现（朴素 DSU + BFS 图遍历）
// 全面交叉核对四种结论。
func TestCorrectRandomCrossCheck(t *testing.T) {
	rng := rand.New(rand.NewSource(987654))
	for iter := 0; iter < 200; iter++ {
		n := 2 + rng.Intn(30)
		net := genConsistentNetwork(t, rng, n, rng.Intn(2*n))

		all := append([]Record{}, net.records...)
		// 一半概率注入一条坏记录（含自比较情形）。
		var injected *Record
		var trueDelta int64
		if rng.Intn(2) == 0 {
			from := net.standards[rng.Intn(n)]
			to := net.standards[rng.Intn(n)]
			trueDelta = net.values[indexOf2(net.standards, to)] - net.values[indexOf2(net.standards, from)]
			perturb := int64(1 + rng.Intn(1000))
			if rng.Intn(2) == 0 {
				perturb = -perturb
			}
			inj := Record{
				ID:    fmt.Sprintf("inj-%06d", iter),
				From:  from,
				To:    to,
				Delta: trueDelta + perturb,
			}
			if from == to && inj.Delta == 0 {
				inj.Delta = perturb
			}
			all = append(all, inj)
			injected = &inj
		}

		maxD := int64(maxDeltaTest)
		if rng.Intn(4) == 0 {
			maxD = int64(1 + rng.Intn(1000)) // 小上限，覆盖 outOfRange 分支
		}

		// 必查注入的坏记录：其余记录一致且全连通，应唯一推定出真实差值。
		if injected != nil {
			c := checkCorrection(t, net, all, injected.ID, maxD)
			if c.Status != StatusSuggested && c.Status != StatusOutOfRange {
				t.Fatalf("iter %d: injected suspect status = %s", iter, c.Status)
			}
			if c.Fix.Delta != trueDelta {
				t.Fatalf("iter %d: suggested %d, true delta %d", iter, c.Fix.Delta, trueDelta)
			}
		}

		// 再随机查一条记录，与独立 oracle 全面对照。
		suspectID := all[rng.Intn(len(all))].ID
		checkCorrection(t, net, all, suspectID, maxD)
	}
}

// checkCorrection 用独立 oracle 全面核对一次纠错查询，返回结论以便追加断言。
func checkCorrection(t *testing.T, net genNetwork, all []Record, suspectID string, maxDelta int64) Correction {
	t.Helper()
	c, err := Correct(net.standards, all, suspectID, maxDelta)
	if err != nil {
		t.Fatalf("Correct(%s): %v", suspectID, err)
	}
	if c.Suspect.ID != suspectID || c.Message == "" {
		t.Fatalf("suspect/message wrong: %+v", c)
	}

	wantStatus, wantConflict, wantDelta := correctOracle(net.standards, all, suspectID, maxDelta)
	if c.Status != wantStatus {
		t.Fatalf("suspect %s: status = %s, oracle says %s", suspectID, c.Status, wantStatus)
	}

	switch c.Status {
	case StatusOtherConflict:
		if c.OtherConflict == nil || c.OtherConflict.Record.ID != wantConflict {
			t.Fatalf("suspect %s: otherConflict = %+v, want record %s", suspectID, c.OtherConflict, wantConflict)
		}
		if c.OtherConflict.Record.ID == suspectID {
			t.Fatalf("suspect %s: conflict is the suspect itself", suspectID)
		}
		if c.Fix != nil {
			t.Fatalf("suspect %s: unexpected fix %+v", suspectID, c.Fix)
		}
		verifyConflict(t, net, restOf(all, suspectID), c.OtherConflict.Record, c.OtherConflict)
	case StatusUndetermined:
		if c.Fix != nil || c.OtherConflict != nil {
			t.Fatalf("suspect %s: undetermined carries fix/conflict: %+v", suspectID, c)
		}
		if _, reachable := bfsPathSum(restOf(all, suspectID), c.Suspect.From, c.Suspect.To); reachable {
			t.Fatalf("suspect %s: endpoints reachable but status undetermined", suspectID)
		}
	case StatusSuggested, StatusOutOfRange:
		if c.Fix == nil || c.Fix.Delta != wantDelta {
			t.Fatalf("suspect %s: fix = %+v, oracle delta %d", suspectID, c.Fix, wantDelta)
		}
		if c.OtherConflict != nil {
			t.Fatalf("suspect %s: unexpected conflict %+v", suspectID, c.OtherConflict)
		}
		if (c.Status == StatusSuggested) != c.Fix.Legal {
			t.Fatalf("suspect %s: status %s but legal=%v", suspectID, c.Status, c.Fix.Legal)
		}
		verifyFix(t, all, c, maxDelta)
	default:
		t.Fatalf("suspect %s: unknown status %q", suspectID, c.Status)
	}
	return c
}

// correctOracle 用测试侧独立的朴素带权并查集复算纠错查询结论，
// 与生产实现交叉验证。假定 suspectID 存在。
func correctOracle(standards []string, records []Record, suspectID string, maxDelta int64) (status, conflictID string, delta int64) {
	var suspect Record
	rest := make([]Record, 0, len(records))
	for _, r := range records {
		if r.ID == suspectID {
			suspect = r
			continue
		}
		rest = append(rest, r)
	}

	sorted := append([]Record{}, rest...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].ID < sorted[j].ID })

	pos := make(map[string]int, len(standards))
	for i, s := range standards {
		pos[s] = i
	}
	d := newNaiveDSU(len(standards))
	for _, r := range sorted {
		if !d.union(pos[r.From], pos[r.To], r.Delta) {
			return StatusOtherConflict, r.ID, 0
		}
	}
	a, b := pos[suspect.From], pos[suspect.To]
	if d.find(a) != d.find(b) {
		return StatusUndetermined, "", 0
	}
	implied := d.diff(a, b)
	if implied > maxDelta || implied < -maxDelta {
		return StatusOutOfRange, "", implied
	}
	return StatusSuggested, "", implied
}

// verifyFix 独立复核“两端连通”结论：路径合法、逐步带符号累计等于建议值，
// 并与测试侧独立图遍历的结果一致。
func verifyFix(t *testing.T, records []Record, c Correction, maxDelta int64) {
	t.Helper()
	if c.Fix == nil {
		t.Fatalf("status %s but fix is nil", c.Status)
	}
	f := c.Fix
	suspect := c.Suspect
	rest := restOf(records, suspect.ID)

	// 独立图遍历求出的差值必须等于建议值。
	want, reachable := bfsPathSum(rest, suspect.From, suspect.To)
	if !reachable {
		t.Fatalf("fix given for %s but %s/%s not connected in remaining records",
			suspect.ID, suspect.From, suspect.To)
	}
	if f.Delta != want {
		t.Fatalf("record %s: suggested delta %d, independent traversal says %d",
			suspect.ID, f.Delta, want)
	}
	if f.Diff != f.Delta-suspect.Delta {
		t.Fatalf("diff = %d, want %d (= delta - original)", f.Diff, f.Delta-suspect.Delta)
	}
	if wantLegal := f.Delta <= maxDelta && f.Delta >= -maxDelta; f.Legal != wantLegal {
		t.Fatalf("legal = %v, want %v (delta %d, max %d)", f.Legal, wantLegal, f.Delta, maxDelta)
	}

	// 路径端点与疑似记录一致。
	p := f.Path
	if p.From != suspect.From || p.To != suspect.To {
		t.Fatalf("path endpoints %s->%s, want %s->%s", p.From, p.To, suspect.From, suspect.To)
	}

	// 逐步核对：每步对应其余记录中的一条真实记录，方向与符号正确，
	// 逐步累计等于 RunningTotal，且路径之和等于建议值。
	var sum int64
	used := map[string]bool{}
	for i, s := range p.Steps {
		if s.RecordID == suspect.ID {
			t.Fatalf("path uses the suspect record %s", s.RecordID)
		}
		if used[s.RecordID] {
			t.Fatalf("record %s reused on path", s.RecordID)
		}
		used[s.RecordID] = true
		rec := findRecord(rest, s.RecordID)
		if rec == nil {
			t.Fatalf("step record %s not among remaining records", s.RecordID)
		}
		switch s.Direction {
		case "forward":
			if s.From != rec.From || s.To != rec.To || s.SignedDelta != rec.Delta {
				t.Fatalf("forward step inconsistent with record: %+v vs %+v", s, rec)
			}
		case "reverse":
			if s.From != rec.To || s.To != rec.From || s.SignedDelta != -rec.Delta {
				t.Fatalf("reverse step inconsistent with record: %+v vs %+v", s, rec)
			}
		default:
			t.Fatalf("bad direction %q", s.Direction)
		}
		if i > 0 && s.From != p.Steps[i-1].To {
			t.Fatal("path steps are not contiguous")
		}
		sum += s.SignedDelta
		if p.RunningTotal[i] != sum {
			t.Fatalf("running total[%d] = %d, want %d", i, p.RunningTotal[i], sum)
		}
	}
	if len(p.Steps) > 0 {
		if p.Steps[0].From != p.From || p.Steps[len(p.Steps)-1].To != p.To {
			t.Fatal("path does not span from/to")
		}
	}
	if p.Total != sum || p.Total != f.Delta {
		t.Fatalf("path total %d, step sum %d, suggested %d", p.Total, sum, f.Delta)
	}
}

// restOf 返回移除指定 id 后的其余记录（保持原顺序，不修改入参）。
func restOf(records []Record, id string) []Record {
	rest := make([]Record, 0, len(records))
	for _, r := range records {
		if r.ID != id {
			rest = append(rest, r)
		}
	}
	return rest
}

// bfsPathSum 是测试侧的独立图遍历：把每条记录当作无向边（顺记录方向
// +delta、逆方向 -delta），从 from 出发 BFS 累计势能，返回到达 to 的
// 路径代数和；网络一致时该值与路径选择无关。reachable 为 false 表示
// 两端在这些记录中不连通。
func bfsPathSum(records []Record, from, to string) (sum int64, reachable bool) {
	type edge struct {
		to string
		w  int64
	}
	adj := map[string][]edge{}
	for _, r := range records {
		adj[r.From] = append(adj[r.From], edge{to: r.To, w: r.Delta})
		adj[r.To] = append(adj[r.To], edge{to: r.From, w: -r.Delta})
	}
	dist := map[string]int64{from: 0}
	queue := []string{from}
	for len(queue) > 0 {
		u := queue[0]
		queue = queue[1:]
		if u == to {
			return dist[u], true
		}
		for _, e := range adj[u] {
			if _, seen := dist[e.to]; !seen {
				dist[e.to] = dist[u] + e.w
				queue = append(queue, e.to)
			}
		}
	}
	return 0, false
}
