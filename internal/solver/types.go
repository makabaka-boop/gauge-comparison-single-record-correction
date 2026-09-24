// Package solver 维护带势能（potential）的并查集，按记录 id 的 UTF-8 字节序
// 逐条检查比较记录，并在发现首个矛盾时从“此前已接受记录”构成的森林中
// 还原可直接对照的有向路径。
package solver

// MaxDelta 是单条记录 delta 的既有合法取值上限（按绝对值计）。
// 纠错查询不得建议超出 [-MaxDelta, MaxDelta] 的非法修正值。
const MaxDelta = 1_000_000_000

// Record 是一条比较记录，固定含义：value[to] - value[from] = Delta。
type Record struct {
	ID    string `json:"id"`
	From  string `json:"from"`
	To    string `json:"to"`
	Delta int64  `json:"delta"`
}

// Step 是有向路径上的一步。
type Step struct {
	RecordID string `json:"recordId"`
	From     string `json:"from"`
	To       string `json:"to"`
	// Direction 为 "forward" 表示按记录方向行走（to - from = delta），
	// 为 "reverse" 表示反向行走（from - to = -delta）。
	Direction string `json:"direction"`
	// SignedDelta 是沿行走方向的累计增量：正向为 delta，反向为 -delta。
	SignedDelta int64 `json:"signedDelta"`
}

// Path 是从 From 到 To 的有向路径，RunningTotal 为对应步的累计差值
// （RunningTotal[k] = 前 k+1 步 SignedDelta 之和，最后一个等于 Total）。
type Path struct {
	From         string  `json:"from"`
	To           string  `json:"to"`
	Steps        []Step  `json:"steps"`
	RunningTotal []int64 `json:"runningTotal"`
	Total        int64   `json:"total"`
}

// Loop 是记录 id 顺序上最先闭合的矛盾环：先走森林路径，再由失效记录闭合。
type Loop struct {
	Nodes       []string `json:"nodes"`
	PathTotal   int64    `json:"pathTotal"`
	RecordDelta int64    `json:"recordDelta"`
	// Sum 为沿闭合环一圈的代数和，PathTotal - RecordDelta；一致时必为 0。
	Sum int64 `json:"sum"`
}

// Conflict 描述最先失效的记录及其与森林路径的对照信息。
type Conflict struct {
	Record Record `json:"record"`
	// ImpliedDelta 是此前已接受记录所隐含的 value[to] - value[from]。
	ImpliedDelta int64 `json:"impliedDelta"`
	// Mismatch 为 Record.Delta - ImpliedDelta，非零即矛盾。
	Mismatch int64 `json:"mismatch"`
	Path     Path  `json:"path"`
	Loop     Loop  `json:"loop"`
}

// Result 是一次约束传播的结论。Consistent 为 true 时 Values 有效；
// 为 false 时 Conflict 指向第一条失效记录。
type Result struct {
	Consistent bool      `json:"consistent"`
	Values     Int64Map  `json:"values,omitempty"`
	Conflict   *Conflict `json:"conflict,omitempty"`
}

// Correction 的状态取值。
const (
	// CorrectionFixed ：移除疑似记录后其余记录一致且两端连通，
	// 存在唯一应填 delta，单改此条即可修复整网。
	CorrectionFixed = "fixed"
	// CorrectionStillConflicting ：移除疑似记录后其余记录仍矛盾，
	// 单改此条无效。
	CorrectionStillConflicting = "stillConflicting"
	// CorrectionUnderdetermined ：其余记录一致，但疑似记录两端分属
	// 不同连通分量，差值无法唯一推定。
	CorrectionUnderdetermined = "underdetermined"
	// CorrectionOutOfRange ：唯一应填 delta 超出既有取值范围，
	// 不提供非法修正建议。
	CorrectionOutOfRange = "outOfRange"
)

// Correction 是“只改疑似记录能否修复整网”的查询结论。
// 查询为纯计算，不改动任何输入记录，也不影响 Solve 的核验结果。
type Correction struct {
	Suspect Record `json:"suspect"`
	Status  string `json:"status"`
	// Note 面向复核员的结论说明。
	Note string `json:"note"`

	// ImpliedDelta 在 fixed/outOfRange 时给出：其余记录隐含的唯一
	// value[to]-value[from]（势能差）。
	ImpliedDelta *int64 `json:"impliedDelta,omitempty"`
	// SuggestedDelta 仅在 fixed 时给出：合法取值范围内的建议修正值，
	// 等于 ImpliedDelta。
	SuggestedDelta *int64 `json:"suggestedDelta,omitempty"`
	// DeltaChange = SuggestedDelta - Suspect.Delta；0 表示原值本就正确。
	DeltaChange *int64 `json:"deltaChange,omitempty"`

	// Path 为其余记录构成的森林中 suspect.From → suspect.To 的唯一路径，
	// 含逐步方向与符号（fixed/outOfRange 时给出；自比较为空路径）。
	Path *Path `json:"path,omitempty"`
	// Conflict 在 stillConflicting 时给出：其余记录中最先失效的另一条
	// 记录及其闭环证据。
	Conflict *Conflict `json:"conflict,omitempty"`
}
