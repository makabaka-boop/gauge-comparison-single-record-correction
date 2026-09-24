// Package solver 维护带势能（potential）的并查集，按记录 id 的 UTF-8 字节序
// 逐条检查比较记录，并在发现首个矛盾时从“此前已接受记录”构成的森林中
// 还原可直接对照的有向路径。
package solver

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

// Correction 是纠错查询的结论：暂时移除疑似记录后，按原 id 顺序复核其余
// 约束，判断只改这一条记录能否修复整张校准网。查询不修改原记录，也不改变
// Solve 的核验结果。
type Correction struct {
	// Suspect 原样回显被查询的疑似记录。
	Suspect Record `json:"suspect"`
	// Status 为 StatusOtherConflict / StatusUndetermined /
	// StatusSuggested / StatusOutOfRange 之一。
	Status string `json:"status"`
	// Message 用一句话说明结论（如“只改此条记录无法修复校准网”）。
	Message string `json:"message"`
	// OtherConflict 仅 Status 为 otherConflict 时给出：其余记录按原 id
	// 顺序复核最先出现的另一条冲突及现有闭环证据。
	OtherConflict *Conflict `json:"otherConflict,omitempty"`
	// Fix 仅疑似记录两端在其余网络中连通时（suggested / outOfRange）给出。
	Fix *Fix `json:"fix,omitempty"`
}

// Fix 是疑似记录两端连通时，由势能差唯一确定的应填值及其证据。
type Fix struct {
	// Delta 是唯一应填 delta：其余记录隐含的 value[to]-value[from]。
	Delta int64 `json:"delta"`
	// Diff 是与原值的差额：Delta - 原记录 delta。
	Diff int64 `json:"diff"`
	// Path 是其余记录构成的森林中 from→to 的有向路径，逐步符号见
	// Steps；Path.Total 恒等于 Delta。
	Path Path `json:"path"`
	// Legal 表示 Delta 是否在既有 delta 取值范围内；为 false 时
	// 本结构仅供证据参考，不构成修正建议。
	Legal bool `json:"legal"`
}
