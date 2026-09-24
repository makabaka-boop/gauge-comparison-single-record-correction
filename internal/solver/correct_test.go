package solver

import (
	"math/rand"
	"reflect"
	"sort"
	"testing"
)

// ---- 独立图遍历 oracle（与生产的并查集/森林实现无关） ----

type bfsEdge struct {
	to    string
	delta int64 // 沿行走方向 value[to] - value[cur]
}

// bfsPotentials 从 start 出发沿全部给定记录（正向、反向）做 BFS，
// 返回各可达节点相对 start 的势能及可达标记。每条无向记录展开为
// 两条有向边：from→to 权 delta、to→from 权 -delta。
func bfsPotentials(records []Record, start string) (map[string]int64, map[string]bool) {
	adj := map[string][]bfsEdge{}
	for _, r := range records {
		if r.From == r.To {
			continue
		}
		adj[r.From] = append(adj[r.From], bfsEdge{to: r.To, delta: r.Delta})
		adj[r.To] = append(adj[r.To], bfsEdge{to: r.From, delta: -r.Delta})
	}
	pot := map[string]int64{start: 0}
	seen := map[string]bool{start: true}
	queue := []string{start}
	for len(queue) > 0 {
		u := queue[0]
		queue = queue[1:]
		for _, e := range adj[u] {
			if seen[e.to] {
				continue
			}
			seen[e.to] = true
			pot[e.to] = pot[u] + e.delta
			queue = append(queue, e.to)
		}
	}
	return pot, seen
}

type oracleOutcome struct {
	status     string
	conflictID string // stillConflicting 时首条失效记录 id
	implied    int64  // fixed/outOfRange 时其余记录隐含的差值
}

// oracleCorrect 用朴素 DSU（独立定位首条失效）+ 独立 BFS（连通性与势能差）
// 推导纠错查询应当返回的结论。
func oracleCorrect(t *testing.T, standards []string, rest []Record, suspect Record) oracleOutcome {
	t.Helper()
	sorted := append([]Record{}, rest...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].ID < sorted[j].ID })

	pos := map[string]int{}
	for i, s := range standards {
		pos[s] = i
	}
	d := newNaiveDSU(len(standards))
	for i := range sorted {
		r := sorted[i]
		if !d.union(pos[r.From], pos[r.To], r.Delta) {
			return oracleOutcome{status: CorrectionStillConflicting, conflictID: r.ID}
		}
	}

	pot, seen := bfsPotentials(rest, suspect.From)
	if !seen[suspect.To] {
		return oracleOutcome{status: CorrectionUnderdetermined}
	}
	implied := pot[suspect.To] - pot[suspect.From]
	if implied > MaxDelta || implied < -MaxDelta {
		return oracleOutcome{status: CorrectionOutOfRange, implied: implied}
	}
	return oracleOutcome{status: CorrectionFixed, implied: implied}
}

// checkCorrection 用独立 oracle 复核 Correct 的全部结论，并验证：
//   - 返回路径之和（逐步带符号累加）等于建议值；
//   - 查询不改动输入记录，也不改变 Solve 的原核验结果。
func checkCorrection(t *testing.T, standards []string, records []Record, suspectID string) Correction {
	t.Helper()

	var suspect Record
	var rest []Record
	found := false
	for _, r := range records {
		if !found && r.ID == suspectID {
			suspect = r
			found = true
			continue
		}
		rest = append(rest, r)
	}
	if !found {
		t.Fatalf("suspect %s not in records", suspectID)
	}

	before := append([]Record{}, records...)
	wantSolve := Solve(standards, records)

	corr, ok := Correct(standards, records, suspectID)
	if !ok {
		t.Fatalf("Correct reports suspect %s missing", suspectID)
	}
	if !reflect.DeepEqual(records, before) {
		t.Fatal("Correct mutated the records slice")
	}
	if got := Solve(standards, records); !reflect.DeepEqual(got, wantSolve) {
		t.Fatal("Correct changed the original check outcome")
	}
	if corr.Suspect != suspect {
		t.Fatalf("suspect = %+v, want %+v", corr.Suspect, suspect)
	}
	if corr.Note == "" {
		t.Fatal("note must be non-empty")
	}

	want := oracleCorrect(t, standards, rest, suspect)
	if corr.Status != want.status {
		t.Fatalf("status = %s, oracle wants %s", corr.Status, want.status)
	}

	switch want.status {
	case CorrectionStillConflicting:
		if corr.Conflict == nil {
			t.Fatal("missing conflict evidence")
		}
		if corr.Conflict.Record.ID == suspect.ID {
			t.Fatal("conflict must point at another record, not the suspect")
		}
		if corr.Conflict.Record.ID != want.conflictID {
			t.Fatalf("conflict = %s, oracle first-fail = %s",
				corr.Conflict.Record.ID, want.conflictID)
		}
		// 闭环证据必须非零且自洽。
		if corr.Conflict.Loop.Sum == 0 ||
			corr.Conflict.Loop.Sum != corr.Conflict.Path.Total-corr.Conflict.Record.Delta {
			t.Fatalf("bad loop evidence: %+v", corr.Conflict.Loop)
		}
		if corr.SuggestedDelta != nil || corr.ImpliedDelta != nil || corr.Path != nil {
			t.Fatal("stillConflicting must not carry a suggested delta or path")
		}
		// 必须与直接对其余记录跑 Solve 的结论完全一致。
		got := Solve(standards, rest)
		if got.Consistent || !reflect.DeepEqual(*got.Conflict, *corr.Conflict) {
			t.Fatal("conflict differs from Solve on remaining records")
		}

	case CorrectionUnderdetermined:
		if corr.SuggestedDelta != nil || corr.ImpliedDelta != nil ||
			corr.Path != nil || corr.Conflict != nil {
			t.Fatal("underdetermined must not carry delta, path or conflict")
		}

	case CorrectionOutOfRange:
		if corr.ImpliedDelta == nil || *corr.ImpliedDelta != want.implied {
			t.Fatalf("implied = %v, oracle wants %d", corr.ImpliedDelta, want.implied)
		}
		if corr.SuggestedDelta != nil || corr.DeltaChange != nil || corr.Conflict != nil {
			t.Fatal("out-of-range must not suggest an illegal correction")
		}
		checkSuggestionPath(t, rest, suspect, corr.Path, want.implied)

	case CorrectionFixed:
		if corr.SuggestedDelta == nil || *corr.SuggestedDelta != want.implied {
			t.Fatalf("suggested = %v, oracle wants %d", corr.SuggestedDelta, want.implied)
		}
		if corr.ImpliedDelta == nil || *corr.ImpliedDelta != want.implied {
			t.Fatalf("implied = %v, want %d", corr.ImpliedDelta, want.implied)
		}
		if corr.DeltaChange == nil || *corr.DeltaChange != want.implied-suspect.Delta {
			t.Fatalf("deltaChange = %v, want %d", corr.DeltaChange, want.implied-suspect.Delta)
		}
		if corr.Conflict != nil {
			t.Fatal("fixed must not carry a conflict")
		}
		checkSuggestionPath(t, rest, suspect, corr.Path, want.implied)
	}
	return corr
}

// checkSuggestionPath 独立核对森林路径：端点正确、只使用剩余记录（绝不含
// 疑似记录）、逐步方向与符号正确、累计值自洽，且路径之和等于建议值。
func checkSuggestionPath(t *testing.T, rest []Record, suspect Record, p *Path, wantTotal int64) {
	t.Helper()
	if p == nil {
		t.Fatal("missing forest path")
	}
	if p.From != suspect.From || p.To != suspect.To {
		t.Fatalf("path endpoints %s->%s, want %s->%s",
			p.From, p.To, suspect.From, suspect.To)
	}
	byID := map[string]Record{}
	for _, r := range rest {
		byID[r.ID] = r
	}
	var sum int64
	for i, s := range p.Steps {
		rec, ok := byID[s.RecordID]
		if !ok {
			t.Fatalf("path step uses record %s outside the remaining records", s.RecordID)
		}
		if s.RecordID == suspect.ID {
			t.Fatal("path must not use the suspect record")
		}
		switch s.Direction {
		case "forward":
			if s.From != rec.From || s.To != rec.To || s.SignedDelta != rec.Delta {
				t.Fatalf("bad forward step %+v vs record %+v", s, rec)
			}
		case "reverse":
			if s.From != rec.To || s.To != rec.From || s.SignedDelta != -rec.Delta {
				t.Fatalf("bad reverse step %+v vs record %+v", s, rec)
			}
		default:
			t.Fatalf("bad direction %q", s.Direction)
		}
		if i > 0 && s.From != p.Steps[i-1].To {
			t.Fatal("path steps are not contiguous")
		}
		sum += s.SignedDelta
		if p.RunningTotal[i] != sum {
			t.Fatalf("runningTotal[%d] = %d, want %d", i, p.RunningTotal[i], sum)
		}
	}
	if len(p.Steps) > 0 {
		if p.Steps[0].From != p.From || p.Steps[len(p.Steps)-1].To != p.To {
			t.Fatal("path does not span suspect endpoints")
		}
	}
	if p.Total != sum || sum != wantTotal {
		t.Fatalf("path sum = %d (path.total = %d), suggested = %d", sum, p.Total, wantTotal)
	}
}

// 反向边：唯一应填差值需逆着记录方向推定。
func TestCorrectReversePath(t *testing.T) {
	standards := []string{"A", "B", "C"}
	records := []Record{
		{ID: "r1", From: "B", To: "A", Delta: -2},  // A - B = -2
		{ID: "r2", From: "C", To: "B", Delta: 5},   // B - C = 5
		{ID: "r3", From: "A", To: "C", Delta: 999}, // 疑似抄错；隐含 C - A = -3
	}
	corr := checkCorrection(t, standards, records, "r3")
	if corr.Status != CorrectionFixed {
		t.Fatalf("status = %s", corr.Status)
	}
	if *corr.SuggestedDelta != -3 || *corr.DeltaChange != -1002 {
		t.Fatalf("suggested = %d change = %d, want -3/-1002",
			*corr.SuggestedDelta, *corr.DeltaChange)
	}
	// 路径 A→B→C 两步都是逆着记录方向行走。
	if len(corr.Path.Steps) != 2 ||
		corr.Path.Steps[0].Direction != "reverse" ||
		corr.Path.Steps[1].Direction != "reverse" ||
		corr.Path.Steps[0].SignedDelta != 2 ||
		corr.Path.Steps[1].SignedDelta != -5 {
		t.Fatalf("path = %+v, want two reverse steps 2, -5", corr.Path)
	}
}

// 平行比较：疑似记录被移除后，同对的另一条记录唯一确定差值。
func TestCorrectParallelRecords(t *testing.T) {
	standards := []string{"A", "B"}
	records := []Record{
		{ID: "p1", From: "A", To: "B", Delta: 10},
		{ID: "p2", From: "A", To: "B", Delta: 11}, // 疑似
	}
	corr := checkCorrection(t, standards, records, "p2")
	if corr.Status != CorrectionFixed || *corr.SuggestedDelta != 10 {
		t.Fatalf("corr = %+v", corr)
	}
	if *corr.DeltaChange != -1 {
		t.Fatalf("deltaChange = %d, want -1", *corr.DeltaChange)
	}
	if len(corr.Path.Steps) != 1 || corr.Path.Steps[0].RecordID != "p1" {
		t.Fatalf("path = %+v, want single p1 step", corr.Path)
	}
	// 反过来怀疑 p1，则应填 11，路径单边走 p2。
	corr2 := checkCorrection(t, standards, records, "p1")
	if corr2.Status != CorrectionFixed || *corr2.SuggestedDelta != 11 {
		t.Fatalf("corr2 = %+v", corr2)
	}
	if len(corr2.Path.Steps) != 1 || corr2.Path.Steps[0].RecordID != "p2" {
		t.Fatalf("path = %+v, want single p2 step", corr2.Path)
	}
}

// 自比较：疑似记录两端相同，唯一合法差值为 0，路径为空。
func TestCorrectSelfComparison(t *testing.T) {
	standards := []string{"A", "B"}
	records := []Record{
		{ID: "r1", From: "A", To: "B", Delta: 4},
		{ID: "s1", From: "A", To: "A", Delta: 3}, // 疑似抄错的自比较
	}
	corr := checkCorrection(t, standards, records, "s1")
	if corr.Status != CorrectionFixed {
		t.Fatalf("status = %s", corr.Status)
	}
	if *corr.SuggestedDelta != 0 || *corr.DeltaChange != -3 {
		t.Fatalf("suggested = %d change = %d, want 0/-3",
			*corr.SuggestedDelta, *corr.DeltaChange)
	}
	if corr.Path.Total != 0 || len(corr.Path.Steps) != 0 {
		t.Fatalf("self comparison path should be empty: %+v", corr.Path)
	}
}

// 桥边：疑似记录是两个分量之间唯一的连接，移除后差值无法唯一推定。
func TestCorrectBridgeUnderdetermined(t *testing.T) {
	standards := []string{"A", "B", "C", "D"}
	records := []Record{
		{ID: "r1", From: "A", To: "B", Delta: 1},
		{ID: "br", From: "B", To: "C", Delta: 7}, // 疑似，但它是桥边
		{ID: "r2", From: "C", To: "D", Delta: 2},
	}
	corr := checkCorrection(t, standards, records, "br")
	if corr.Status != CorrectionUnderdetermined {
		t.Fatalf("status = %s, want underdetermined", corr.Status)
	}
	// 独立 BFS 复核：移除桥边后 B、C 确不连通。
	rest := []Record{records[0], records[2]}
	_, seenFrom := bfsPotentials(rest, "B")
	if seenFrom["C"] {
		t.Fatal("independent BFS says B,C still connected after bridge removal")
	}
}

// 多处矛盾：移除疑似记录后其余记录仍矛盾，单改此条无效；
// 返回字节序下最先失效的另一条记录及闭环证据。
func TestCorrectStillConflicting(t *testing.T) {
	standards := []string{"A", "B", "C"}
	records := []Record{
		{ID: "r1", From: "A", To: "B", Delta: 2},
		{ID: "r2", From: "B", To: "C", Delta: 3},
		{ID: "r3", From: "A", To: "C", Delta: 4},  // 疑似（确为坏记录）
		{ID: "r4", From: "A", To: "B", Delta: 99}, // 还有另一条坏记录
	}
	corr := checkCorrection(t, standards, records, "r3")
	if corr.Status != CorrectionStillConflicting {
		t.Fatalf("status = %s", corr.Status)
	}
	// 其余记录中 r4 与 r1 平行矛盾，按字节序 r4 最先失效。
	if corr.Conflict.Record.ID != "r4" || corr.Conflict.ImpliedDelta != 2 {
		t.Fatalf("conflict = %+v", corr.Conflict)
	}
	if corr.Conflict.Path.Total != 2 || corr.Conflict.Loop.Sum != 2-99 {
		t.Fatalf("path/loop = %+v/%+v", corr.Conflict.Path, corr.Conflict.Loop)
	}
	// 结论必须明确“单改此条无效”。
	if corr.Note == "" || !contains(corr.Note, "无法修复") {
		t.Fatalf("note must state single-record fix is ineffective: %q", corr.Note)
	}
}

// 超出既有 delta 取值范围：唯一应填值合法范围之外，不得给修正建议。
func TestCorrectOutOfRange(t *testing.T) {
	standards := []string{"A", "B", "C"}
	records := []Record{
		{ID: "r1", From: "A", To: "B", Delta: 1_000_000_000},
		{ID: "r2", From: "B", To: "C", Delta: 1_000_000_000},
		{ID: "r3", From: "A", To: "C", Delta: 5}, // 疑似；隐含 2e9，越界
	}
	corr := checkCorrection(t, standards, records, "r3")
	if corr.Status != CorrectionOutOfRange {
		t.Fatalf("status = %s", corr.Status)
	}
	if corr.ImpliedDelta == nil || *corr.ImpliedDelta != 2_000_000_000 {
		t.Fatalf("implied = %v, want 2000000000", corr.ImpliedDelta)
	}
	// 路径之和仍须等于隐含值（2e9），只是不能作为建议修正。
	if corr.Path.Total != 2_000_000_000 {
		t.Fatalf("path total = %d, want 2000000000", corr.Path.Total)
	}
}

// 疑似记录实际没有抄错：建议值等于原值，差额为 0。
func TestCorrectNoChangeNeeded(t *testing.T) {
	standards := []string{"A", "B", "C"}
	records := []Record{
		{ID: "r1", From: "A", To: "B", Delta: 2},
		{ID: "r2", From: "B", To: "C", Delta: 3},
		{ID: "r3", From: "A", To: "C", Delta: 5},
	}
	corr := checkCorrection(t, standards, records, "r3")
	if corr.Status != CorrectionFixed {
		t.Fatalf("status = %s", corr.Status)
	}
	if *corr.SuggestedDelta != 5 || *corr.DeltaChange != 0 {
		t.Fatalf("suggested = %d change = %d, want 5/0",
			*corr.SuggestedDelta, *corr.DeltaChange)
	}
}

// 疑似记录是唯一一条记录：移除后两端不连通（自比较除外）。
func TestCorrectOnlyRecordUnderdetermined(t *testing.T) {
	corr := checkCorrection(t,
		[]string{"A", "B"},
		[]Record{{ID: "r1", From: "A", To: "B", Delta: 9}},
		"r1")
	if corr.Status != CorrectionUnderdetermined {
		t.Fatalf("status = %s", corr.Status)
	}
}

func TestCorrectUnknownSuspect(t *testing.T) {
	if _, ok := Correct([]string{"A", "B"},
		[]Record{{ID: "r1", From: "A", To: "B", Delta: 1}}, "nope"); ok {
		t.Fatal("unknown suspect must report ok=false")
	}
}

// 随机网络：注入一到两条坏记录，用独立 oracle 复核纠错查询的全部结论，
// 包括路径之和等于建议值。
func TestCorrectRandomNetworks(t *testing.T) {
	rng := rand.New(rand.NewSource(20260924))
	statusSeen := map[string]bool{}
	for iter := 0; iter < 200; iter++ {
		n := 2 + rng.Intn(40)
		net := genConsistentNetwork(t, rng, n, rng.Intn(n*2))
		all := append([]Record{}, net.records...)

		// 第一条坏记录：改一条已有记录的 delta（加 1..1000，必与真值不同）。
		victim := rng.Intn(len(all))
		all[victim] = Record{
			ID:    all[victim].ID,
			From:  all[victim].From,
			To:    all[victim].To,
			Delta: all[victim].Delta + int64(1+rng.Intn(1000)),
		}

		// 一半概率再注入第二条坏记录，制造多处矛盾。
		if rng.Intn(2) == 0 && len(all) > 1 {
			v2 := rng.Intn(len(all))
			for v2 == victim {
				v2 = rng.Intn(len(all))
			}
			all[v2] = Record{
				ID:    all[v2].ID,
				From:  all[v2].From,
				To:    all[v2].To,
				Delta: all[v2].Delta + int64(1+rng.Intn(1000)),
			}
		}

		suspectID := all[victim].ID
		// 乱序提交，Correct 内部仍必须按 id 字节序复核。
		rng.Shuffle(len(all), func(i, j int) { all[i], all[j] = all[j], all[i] })
		corr := checkCorrection(t, net.standards, all, suspectID)
		statusSeen[corr.Status] = true
	}
	for _, s := range []string{
		CorrectionFixed, CorrectionStillConflicting, CorrectionUnderdetermined,
	} {
		if !statusSeen[s] {
			t.Fatalf("random suite never exercised status %s", s)
		}
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
