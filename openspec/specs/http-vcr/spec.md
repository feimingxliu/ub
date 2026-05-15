# http-vcr Specification

## Purpose

Define HTTP request recording and replay behavior for provider integration tests.

## Requirements

### Requirement: RoundTripper 模式

系统 SHALL 在 `internal/vcr` 中提供实现 `http.RoundTripper` 的 recorder。recorder MUST 支持 `record`、`replay`、`disabled` 三种模式；`UB_VCR` MUST 能解析为对应模式，未设置时默认为 `disabled`。

#### Scenario: disabled 模式透传

- **WHEN** recorder 以 `disabled` 模式包装一个 base transport
- **THEN** `RoundTrip` MUST 直接调用 base transport，且 MUST NOT 读写 cassette 文件

#### Scenario: UB_VCR 解析 record

- **GIVEN** 环境变量 `UB_VCR=record`
- **WHEN** 调用 vcr 的环境变量解析函数
- **THEN** 返回模式为 `record`

#### Scenario: 非法模式报错

- **WHEN** 使用不属于 `record`、`replay`、`disabled` 的模式创建 recorder
- **THEN** 创建函数 MUST 返回 error

### Requirement: Cassette JSONL 格式

系统 SHALL 使用 JSONL cassette 文件保存 HTTP 交互。每行 MUST 是一个 JSON 对象，包含 `request` 和 `response`；request MUST 包含 method、url、headers、body_sha256；response MUST 包含 status_code、headers、body。

#### Scenario: record 写入单行 JSON

- **WHEN** record 模式完成一次 HTTP 请求
- **THEN** cassette 文件新增一行合法 JSON，且该 JSON 包含 request 与 response 字段

#### Scenario: response body 使用 base64

- **WHEN** record 模式保存响应 body
- **THEN** cassette 中的 response body MUST 是 base64 字符串

### Requirement: Record 模式

record 模式 SHALL 调用真实 base transport，并把请求 metadata 与响应保存到 cassette。record MUST 在读取请求 / 响应 body 后恢复 body，使调用方仍可正常读取响应。

#### Scenario: record 调用真实服务

- **GIVEN** 一个 `httptest.Server`
- **WHEN** 使用 record 模式请求该 server
- **THEN** server 收到真实请求，调用方收到真实响应，cassette 中保存该交互

#### Scenario: record 后 response body 可读

- **WHEN** record 模式返回响应给调用方
- **THEN** 调用方读取 response body 得到真实响应内容

### Requirement: Replay 模式

replay 模式 SHALL 从 cassette 顺序读取记录，并按 method、url、body_sha256 匹配请求。匹配成功时系统 MUST 不访问网络，直接返回 cassette 中的响应；匹配失败时 MUST 返回包含 expected 与 actual 信息的 error。

#### Scenario: replay 返回 cassette 响应

- **GIVEN** cassette 中已有一条匹配请求记录
- **WHEN** replay 模式收到相同 method、url、body 的请求
- **THEN** 返回 cassette 中的 status、headers 和 body，不调用 base transport

#### Scenario: replay 请求不匹配

- **GIVEN** cassette 下一条记录是 `POST /v1/messages`
- **WHEN** replay 模式收到 `GET /v1/models`
- **THEN** `RoundTrip` 返回 error，error 信息包含 expected 和 actual

#### Scenario: replay cassette 耗尽

- **GIVEN** cassette 中没有剩余记录
- **WHEN** replay 模式收到请求
- **THEN** `RoundTrip` 返回表示 cassette exhausted 的 error

### Requirement: Header 脱敏

系统 SHALL 在写入 cassette 前脱敏敏感 header。header 名匹配 MUST 大小写不敏感；至少 MUST 覆盖 `Authorization`、`Proxy-Authorization`、`x-api-key`、`api-key`、`x-auth-token`、`x-api-token`、`Cookie`、`Set-Cookie`。

#### Scenario: Authorization 被脱敏

- **WHEN** record 模式录制带 `Authorization: Bearer secret` 的请求
- **THEN** cassette 中该 header 的值为 `***`，且不包含 `secret`

#### Scenario: 非敏感 header 保留

- **WHEN** record 模式录制带 `Content-Type: application/json` 的请求
- **THEN** cassette 中保留 `Content-Type: application/json`

### Requirement: Record 后 replay 往返

系统 SHALL 支持用同一个 cassette 先 record 再 replay 相同请求。replay 返回的 status、headers 和 body MUST 与 record 时保存的响应一致。

#### Scenario: httptest record replay

- **WHEN** 测试先对 `httptest.Server` 使用 record 模式写入 cassette，再用 replay 模式发送相同请求
- **THEN** replay 响应与原响应一致，且 replay 阶段不需要 server 可用
