# 热处理工单装炉分批服务（heattreat）

纯后端 JSON API：将一批**按入厂顺序排列**的热处理工单，恰好切成指定炉数的
连续区间。先最小化各炉总重量的最大值（峰值），峰值相同时取**切分下标序列
字典序最小**的方案，保证计划员拿到稳定唯一的装炉边界。全程不重排工单。

## 规则

- 工单必须连续切分，恰好分成 `batchCount` 炉，每炉件数在
  `[minItems, maxItems]` 之间。
- 优化目标：
  1. 最小化各炉总重量最大值 `peak`（二分阈值 + 滑动窗口 DP 判定可行性）；
  2. 峰值并列时，取切分点序列 `cuts` 字典序最小者（后缀可达 DP + 贪心取最小切点）。
- 不枚举任何切法组合，时间复杂度约 O(k·n·log W)。

## 约束

| 字段 | 范围 |
| --- | --- |
| `weights` | 1–600 个正整数，每个 ≤ 10^6 |
| `batchCount` | 1–30 |
| `minItems` / `maxItems` | ≥ 1，且 `minItems ≤ maxItems` |

未知字段、非法 JSON、零/负重量、字段缺失或边界不一致返回 **422**；
炉数与件数约束无法同时满足（`minItems*k > n` 或 `maxItems*k < n`）返回
**409 / IMPOSSIBLE_PARTITION**。

## 运行

```bash
docker compose up --build
```

服务监听 `:8080`。

### 请求

```bash
curl -s -X POST http://localhost:8080/partition \
  -H 'Content-Type: application/json' \
  -d '{"weights":[5,1,4,2,3],"batchCount":3,"minItems":1,"maxItems":5}'
```

### 响应（200）

```json
{
  "batches": [
    {"start": 0, "end": 1, "weight": 5},
    {"start": 1, "end": 3, "weight": 5},
    {"start": 3, "end": 5, "weight": 5}
  ],
  "cuts": [1, 3],
  "peak": 5,
  "items": 5
}
```

`start`/`end` 为左闭右开下标；`cuts` 为内部切分点；`peak` 为各炉重量峰值。

### 错误

```json
{ "error": "IMPOSSIBLE_PARTITION", "message": "cannot split 5 orders into 3 batches of 2..4 items each" }
```

```json
{ "error": "VALIDATION_ERROR", "message": "weights[1] must be a positive integer" }
```

## 本地开发

```bash
go test ./... -race -count=1
go run ./cmd/api
```

测试对小规模输入用组合枚举穷举所有切点作为对拍 oracle，覆盖单炉、每炉固定
件数、大量并列切法（全等重量）以及数千组随机用例，并校验 600 件上限输入的
性能与重复调用结果稳定性。
