# gaugeblock — 量块校准网一致性检查

计量室用成对比较传递量块相对偏差。一条抄反符号的记录会让整张校准网
自相矛盾，而只检查局部三角形会漏掉更长的闭合回路。本服务把每条比较记录
解释为一个等式约束

```
value[to] - value[from] = delta
```

用带势能（potential）的并查集做全局约束传播，**按记录 id 的 UTF-8 字节序**
逐条处理：全部一致时给出以每个连通分量最小标准件为零点的全部相对值；
一旦矛盾，定位到**最先失效的记录**，并从“此前已接受记录”构成的森林里
还原 `from → to` 的有向路径、逐步符号与累计差值，复核员可以把路径总差
和失效记录的 delta 直接对照，得到一个确定的矛盾闭环。

当计量员怀疑**某一条记录的差值抄错**时，`/correct` 纠错查询先暂时移除该
记录并按原 id 顺序复核其余约束：其余记录仍矛盾则返回最先出现的另一条
冲突（单改此条无效）；其余一致但疑似记录两端不连通则报告差值无法唯一
推定；两端已连通则用势能差求出唯一应填 delta，附森林路径、逐步符号与
和原值的差额，应填值超出 delta 取值范围时不提供非法修正建议。查询只读，
不修改原记录或原核验结果。

## 技术栈

- Go 1.23，仅标准库 `net/http`，纯后端 JSON API，无外部依赖
- Docker / Docker Compose 运行（多阶段构建，distroless 非 root 镜像）
- 测试使用与生产实现相互独立的朴素带权并查集交叉验证

## 目录结构

```
cmd/api/             服务入口（PORT 环境变量，默认 8080）
internal/solver/     带势能并查集 + 森林路径/矛盾环诊断 + 单记录纠错查询
internal/api/        JSON API 与结构校验（422）
Dockerfile           多阶段构建
docker-compose.yml   api 服务
```

## 运行

```bash
docker compose up --build
# 或本地
go run ./cmd/api
```

## API

### `GET /healthz`

```json
{"status":"ok"}
```

### `POST /check`

请求：

| 字段 | 说明 |
| --- | --- |
| `standards` | 2–2000 个唯一 ASCII 标准件 id（非空可见 ASCII，0x21–0x7e） |
| `records` | 至多 6000 条比较，记录 id 唯一；`from`/`to` 必须引用已有标准件 |
| `records[].delta` | 整数，绝对值 ≤ 10^9 |

结构错误（JSON 非法、未知字段、数量越界、id 重复、端点不存在、
delta 越界等）返回 **422**，且不进入任何计算。

一致时（HTTP 200）：

```json
{
  "consistent": true,
  "values": {"A": 0, "B": 2, "C": 5}
}
```

`values` 以每个连通分量中 id 最小的标准件为零点，键按 id 排序输出。

矛盾时（HTTP 200，业务结果）：

```json
{
  "consistent": false,
  "conflict": {
    "record": {"id": "r5", "from": "A", "to": "E", "delta": 20},
    "impliedDelta": 18,
    "mismatch": 2,
    "path": {
      "from": "A",
      "to": "E",
      "steps": [
        {"recordId": "r1", "from": "A", "to": "B", "direction": "forward", "signedDelta": 1},
        {"recordId": "r2", "from": "B", "to": "C", "direction": "reverse", "signedDelta": 3},
        {"recordId": "r3", "from": "C", "to": "D", "direction": "forward", "signedDelta": 10},
        {"recordId": "r4", "from": "D", "to": "E", "direction": "reverse", "signedDelta": 4}
      ],
      "runningTotal": [1, 4, 14, 18],
      "total": 18
    },
    "loop": {
      "nodes": ["A", "B", "C", "D", "E", "A"],
      "pathTotal": 18,
      "recordDelta": 20,
      "sum": -2
    }
  }
}
```

对照方法：沿 `path` 走一圈再由失效记录闭合，`loop.sum = pathTotal - recordDelta`
非零即为闭合误差；`steps[].direction` 为 `reverse` 时符号已取反
（`signedDelta = -delta`），所以逐步 `signedDelta` 累加必然等于 `total`。
自比较矛盾（`from == to` 且 delta 非 0）时路径为空，环退化为单点环。

### `POST /correct`

纠错查询：给定与 `/check` 相同的网络和一个疑似记录 id，判断**只改这一条
记录能否修复整张校准网**。请求在 `/check` 字段之外增加：

| 字段 | 说明 |
| --- | --- |
| `suspectId` | 疑似抄错差值的记录 id，必须非空且存在，否则返回 **422** |

结论 `status` 有四种：

**1. `otherConflict` — 单改此条无效。** 移除疑似记录后其余记录仍矛盾，
`otherConflict` 给出按 id 字节序最先出现的**另一条**冲突，结构与
`/check` 的 `conflict` 相同（隐含差、mismatch、森林路径、闭环证据）：

```json
{
  "suspect": {"id": "m3-bad", "from": "A", "to": "C", "delta": 9},
  "status": "otherConflict",
  "message": "移除记录 m3-bad 后其余记录仍矛盾（按 id 顺序首条失效记录为 m5-bad），只改此条记录无法修复校准网",
  "otherConflict": {
    "record": {"id": "m5-bad", "from": "E", "to": "D", "delta": 7},
    "impliedDelta": -1,
    "mismatch": 8,
    "path": {
      "from": "E",
      "to": "D",
      "steps": [
        {"recordId": "m4", "from": "E", "to": "D", "direction": "reverse", "signedDelta": -1}
      ],
      "runningTotal": [-1],
      "total": -1
    },
    "loop": {"nodes": ["E", "D", "E"], "pathTotal": -1, "recordDelta": 7, "sum": -8}
  }
}
```

**2. `undetermined` — 差值无法唯一推定。** 其余记录一致，但疑似记录两端
在其余网络中不连通（如它是连接两个分量的唯一桥边），delta 取任何值都
不会与其余记录冲突：

```json
{
  "suspect": {"id": "br", "from": "B", "to": "C", "delta": 5},
  "status": "undetermined",
  "message": "其余记录一致，但移除记录 br 后 B 与 C 不连通，差值无法由其余记录唯一推定"
}
```

**3. `suggested` — 唯一应填值。** 两端连通，势能差
`value[to] - value[from]` 唯一确定应填 delta；`fix.diff` 是与原值的差额
（`delta - 原值`），`fix.path` 是其余记录森林中 `from → to` 的有向路径，
逐步 `signedDelta` 累加等于 `fix.delta`。`diff` 为 0 表示原值本就正确、
无需修改：

```json
{
  "suspect": {"id": "r3", "from": "A", "to": "C", "delta": 4},
  "status": "suggested",
  "message": "建议将记录 r3 的 delta 改为 5（与原值相差 +1）",
  "fix": {
    "delta": 5,
    "diff": 1,
    "legal": true,
    "path": {
      "from": "A",
      "to": "C",
      "steps": [
        {"recordId": "r1", "from": "A", "to": "B", "direction": "forward", "signedDelta": 2},
        {"recordId": "r2", "from": "B", "to": "C", "direction": "forward", "signedDelta": 3}
      ],
      "runningTotal": [2, 5],
      "total": 5
    }
  }
}
```

**4. `outOfRange` — 不建议非法修正。** 唯一应填 delta 超出既有取值范围
`[-10^9, 10^9]` 时仍返回路径证据（`fix.legal` 为 `false`），但状态明确
标记为超界，不构成修正建议：

```json
{
  "suspect": {"id": "o3", "from": "A", "to": "C", "delta": 0},
  "status": "outOfRange",
  "message": "唯一应填 delta 为 2000000000，超出既有取值范围 [-1000000000, 1000000000]，不提供非法修正建议",
  "fix": {"delta": 2000000000, "diff": 2000000000, "legal": false, "path": { ... }}
}
```

纠错查询为只读操作：不修改任何输入记录，也不改变 `/check` 的核验结果。

## 算法要点

- **带势能并查集**：`pot[x] = value[x] - value[parent]`，路径压缩时累加势能；
  按大小合并。同根记录立即用 `pot[to] - pot[from]` 校验，跨分量记录合并。
- **确定性的“最先失效”**：记录先按 id 的 UTF-8 字节序排序再处理，
  与提交顺序无关。
- **诊断用的森林**：只有真正连接两个分量的已接受记录进入无向森林，
  因此任意两端之间路径唯一；同根的平行比较/闭合弦不进森林，不会污染路径。
- **零点选择**：每个连通分量以 id 最小的标准件为 0，相对值为
  `pot[x] - pot[zero]`。

## 测试

```bash
go test -race ./...
```

覆盖：

- 随机生成以生成树为骨架的一致网络（含随机弦、反向边、平行边、
  合法自比较），独立复核每条约束与各分量零点；
- 注入单条冲突，用**独立的朴素带权并查集**确认报告的就是字节序下
  首条失效记录，并逐步复核路径方向、符号、累计差与闭合环；
- 反向边、平行比较、自比较、输入乱序、记录 id 的 UTF-8 字节序；
- 纠错查询的反向路径、平行记录、自比较、**桥边（不连通）**、
  **多处矛盾（返回另一条冲突）**、超界不给非法建议、查询不改原记录；
  随机网络下用**独立朴素 DSU + 独立 BFS 图遍历**核对四种结论，
  并验证返回路径带符号之和等于建议 delta；
- 2000 标准件 / 6000 记录的大规模场景；
- HTTP 层全部结构错误均为 422（含 `suspectId` 缺失或不存在）。
