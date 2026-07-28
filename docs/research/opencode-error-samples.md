# OpenCode 错误样例

调查日期：2026-07-28。公开资料没有声明稳定的错误响应契约，以下样例只用于建立保守的内置规则和测试夹具；用户必须能够覆盖预设规则。

## 服务信息

- [Models.dev](https://models.dev/api.json) 将 OpenCode Go 标记为 `@ai-sdk/openai-compatible`，默认地址为 `https://opencode.ai/zen/go/v1`，部分模型会覆盖为 OpenAI 或 Anthropic SDK。

## 额度不足

- [OpenCode issue #36372](https://github.com/anomalyco/opencode/issues/36372) 展示 `GoUsageLimitError`，消息为五小时额度已用尽。OpenCode Go 预设应将明确出现该类型的响应判为额度不足。
- [OpenCode issue #32971](https://github.com/anomalyco/opencode/issues/32971) 展示 `FreeUsageLimitError`。OpenCode Zen 预设可以将明确出现该类型的响应判为额度不足。

## 瞬时故障

- [OpenCode issue #34898](https://github.com/anomalyco/opencode/issues/34898) 展示 HTTP `429`、错误码 `provider_rate_limit_exceeded`。这是模型供应方限速，不能据此将用户凭证判为额度不足。
- [OpenCode issue #34903](https://github.com/anomalyco/opencode/issues/34903) 展示 HTTP `503`、错误码 `failover_exhausted`。这是上游推理暂不可用，不能改变凭证状态。

## 规则约束

- OpenCode 预设不能仅凭 HTTP `429`、`503` 或消息中的 `rate limit` 判定额度不足。
- 内置额度规则应匹配明确的错误类型，未知响应保持无结论并通过脱敏调试日志辅助用户调整规则。
