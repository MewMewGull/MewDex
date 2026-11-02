# Hermes Agent messaging reference

- Upstream: https://github.com/NousResearch/hermes-agent
- Revision: `f0d94882e89ab8d4098b5bb2f166adfb9ee0a0ab`
- License: MIT; copyright (c) 2025 Nous Research. See [LICENSE](LICENSE).
- Referenced sources: `gateway/platforms/base.py`, `gateway/platforms/event.py`,
  `gateway/session.py`, `plugins/platforms/weixin/adapter.py`,
  `plugins/platforms/qqbot/adapter.py`, `plugins/platforms/qqbot/send.py`,
  `plugins/platforms/feishu/adapter.py`, and their messaging setup documentation.
- MewDex uses the normalized envelope / adapter / per-chat session arrangement,
  Weixin context-token and QR flow, QQ gateway lifecycle and passive reply
  identifiers, and Feishu bot mention and markdown message conventions.
- Runtime code is implemented in Go under `internal/messaging`; no Hermes Python
  runtime, model execution, credentials, or configuration is bundled.
