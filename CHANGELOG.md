# Changelog

## [v0.0.19](https://github.com/kimata1007/codex-runner-controller/compare/v0.0.18...v0.0.19) - 2026-11-25

- Mermaid の図を SVG 画像に変換するときに配色を保持する by @kimata1007 in https://github.com/kimata1007/codex-runner-controller/pull/142

## [v0.0.18](https://github.com/kimata1007/codex-runner-controller/compare/v0.0.17...v0.0.18) - 2026-11-25

- Queue messages while a session is busy by @kimata1007 in https://github.com/kimata1007/codex-runner-controller/pull/132
- Add extensible AI output rendering to the PWA by @kimata1007 in https://github.com/kimata1007/codex-runner-controller/pull/135
- PWAに作業の途中経過を表示する by @kimata1007 in https://github.com/kimata1007/codex-runner-controller/pull/131

## [v0.0.17](https://github.com/kimata1007/codex-runner-controller/compare/v0.0.16...v0.0.17) - 2026-11-22

- feat: restore active Codex run streams after PWA reload by @kimata1007 in https://github.com/kimata1007/codex-runner-controller/pull/89
- feat: safely archive inactive conversations after seven days by @kimata1007 in https://github.com/kimata1007/codex-runner-controller/pull/95

## [v0.0.16](https://github.com/kimata1007/codex-runner-controller/compare/v0.0.15...v0.0.16) - 2026-11-22

- fix: prevent stale PWA assets and refresh ChatGPT auth status by @kimata1007 in https://github.com/kimata1007/codex-runner-controller/pull/128

## [v0.0.15](https://github.com/kimata1007/codex-runner-controller/compare/v0.0.14...v0.0.15) - 2026-11-22

- refactor: remove unused Codex settings UI and API by @kimata1007 in https://github.com/kimata1007/codex-runner-controller/pull/126

## [v0.0.14](https://github.com/kimata1007/codex-runner-controller/compare/v0.0.13...v0.0.14) - 2026-11-22

- fix: run Codex with Kubernetes Pod isolation instead of bwrap by @kimata1007 in https://github.com/kimata1007/codex-runner-controller/pull/124

## [v0.0.13](https://github.com/kimata1007/codex-runner-controller/compare/v0.0.12...v0.0.13) - 2026-11-19

- fix: hide archived conversations from history list by @kimata1007 in https://github.com/kimata1007/codex-runner-controller/pull/121

## [v0.0.12](https://github.com/kimata1007/codex-runner-controller/compare/v0.0.11...v0.0.12) - 2026-11-18

- fix: archive conversations and preserve history by @kimata1007 in https://github.com/kimata1007/codex-runner-controller/pull/117
- feat: HTTPトレースに安全なルート名とエラー情報を追加 by @kimata1007 in https://github.com/kimata1007/codex-runner-controller/pull/116

## [v0.0.11](https://github.com/kimata1007/codex-runner-controller/compare/v0.0.10...v0.0.11) - 2026-11-18

- feat: PWAからCodex App Serverの設定とアプリツール状態を確認 by @kimata1007 in https://github.com/kimata1007/codex-runner-controller/pull/114

## [v0.0.10](https://github.com/kimata1007/codex-runner-controller/compare/v0.0.9...v0.0.10) - 2026-11-18

- feat: delete conversations from history with menu and long-press by @kimata1007 in https://github.com/kimata1007/codex-runner-controller/pull/102
- Open and close a fresh libSQL stream per request by @kimata1007 in https://github.com/kimata1007/codex-runner-controller/pull/111
- fix: pass Go CI after stream lifecycle change by @kimata1007 in https://github.com/kimata1007/codex-runner-controller/pull/113
- feat: Codex App Serverのstderrを安全にオプトイン転送 by @kimata1007 in https://github.com/kimata1007/codex-runner-controller/pull/112

## [v0.0.9](https://github.com/kimata1007/codex-runner-controller/compare/v0.0.8...v0.0.9) - 2026-11-17

- Retry expired libSQL streams by @kimata1007 in https://github.com/kimata1007/codex-runner-controller/pull/108
- fix: reset libsql baton on transport errors by @kimata1007 in https://github.com/kimata1007/codex-runner-controller/pull/91

## [v0.0.8](https://github.com/kimata1007/codex-runner-controller/compare/v0.0.7...v0.0.8) - 2026-11-17

- feat: standardize structured console logs by @kimata1007 in https://github.com/kimata1007/codex-runner-controller/pull/104
- test: KindでKubernetes E2EとSecret欠落検出を追加 by @kimata1007 in https://github.com/kimata1007/codex-runner-controller/pull/107
- feat: Runner API・Controller・Agent・CodexのOpenTelemetryトレース by @kimata1007 in https://github.com/kimata1007/codex-runner-controller/pull/106

## [v0.0.7](https://github.com/kimata1007/codex-runner-controller/compare/v0.0.6...v0.0.7) - 2026-11-16

- fix: set Codex approval policy to never by @kimata1007 in https://github.com/kimata1007/codex-runner-controller/pull/99
- feat: add correlated error diagnostics without logging secrets by @kimata1007 in https://github.com/kimata1007/codex-runner-controller/pull/100

## [v0.0.6](https://github.com/kimata1007/codex-runner-controller/compare/v0.0.5...v0.0.6) - 2026-11-15

- fix: use numeric runner-agent UID by @kimata1007 in https://github.com/kimata1007/codex-runner-controller/pull/97

## [v0.0.5](https://github.com/kimata1007/codex-runner-controller/compare/v0.0.4...v0.0.5) - 2026-11-15

- fix: use current runner agent for auth broker by @kimata1007 in https://github.com/kimata1007/codex-runner-controller/pull/81
- docs: explain required ChatGPT auth secret by @kimata1007 in https://github.com/kimata1007/codex-runner-controller/pull/82
- fix: keep control tokens out of app server by @kimata1007 in https://github.com/kimata1007/codex-runner-controller/pull/83
- fix: honor session PVC retention policy by @kimata1007 in https://github.com/kimata1007/codex-runner-controller/pull/84
- fix: report failed and interrupted turns by @kimata1007 in https://github.com/kimata1007/codex-runner-controller/pull/85
- feat: restore pending approval cards by @kimata1007 in https://github.com/kimata1007/codex-runner-controller/pull/86
- fix: serialize runner event forwarding by @kimata1007 in https://github.com/kimata1007/codex-runner-controller/pull/87
- fix: project explicit turn terminal events by @kimata1007 in https://github.com/kimata1007/codex-runner-controller/pull/88
- fix: recreate terminal session pods by @kimata1007 in https://github.com/kimata1007/codex-runner-controller/pull/90
- docs: align guides with current API contract by @kimata1007 in https://github.com/kimata1007/codex-runner-controller/pull/92
- fix: reject overlapping runner turns by @kimata1007 in https://github.com/kimata1007/codex-runner-controller/pull/93
- fix: keep SSE streams open for long runs by @kimata1007 in https://github.com/kimata1007/codex-runner-controller/pull/94

## [v0.0.4](https://github.com/kimata1007/codex-runner-controller/compare/v0.0.3...v0.0.4) - 2026-11-15

- feat: share ChatGPT authentication across conversations by @kimata1007 in https://github.com/kimata1007/codex-runner-controller/pull/79

## [v0.0.3](https://github.com/kimata1007/codex-runner-controller/compare/v0.0.2...v0.0.3) - 2026-11-15

- fix: remove user-facing API bearer token by @kimata1007 in https://github.com/kimata1007/codex-runner-controller/pull/77
- feat: polish runner chat by @kimata1007 in https://github.com/kimata1007/codex-runner-controller/pull/76

## [v0.0.2](https://github.com/kimata1007/codex-runner-controller/compare/v0.0.1...v0.0.2) - 2026-11-14

- Scope controller cache to its namespace by @kimata1007 in https://github.com/kimata1007/codex-runner-controller/pull/74

## [v0.0.1](https://github.com/kimata1007/codex-runner-controller/commits/v0.0.1) - 2026-11-14

- feat: add initial runner api contract by @kimata1007 in https://github.com/kimata1007/codex-runner-controller/pull/1
- refactor: add conversation repository boundary by @kimata1007 in https://github.com/kimata1007/codex-runner-controller/pull/2
- feat: define conversation data schema by @kimata1007 in https://github.com/kimata1007/codex-runner-controller/pull/3
- feat: add libsql http repository by @kimata1007 in https://github.com/kimata1007/codex-runner-controller/pull/4
- feat: wire libsql repository into runner api by @kimata1007 in https://github.com/kimata1007/codex-runner-controller/pull/5
- test: cover first message insertion by @kimata1007 in https://github.com/kimata1007/codex-runner-controller/pull/6
- ci: run Go tests on pull requests by @kimata1007 in https://github.com/kimata1007/codex-runner-controller/pull/66
- feat: define codexsession contract by @kimata1007 in https://github.com/kimata1007/codex-runner-controller/pull/7
- feat: build session pod and pvc resources by @kimata1007 in https://github.com/kimata1007/codex-runner-controller/pull/8
- feat: add session reconcile logic by @kimata1007 in https://github.com/kimata1007/codex-runner-controller/pull/9
- feat: connect codexsession to controller runtime by @kimata1007 in https://github.com/kimata1007/codex-runner-controller/pull/10
- feat: add app server stdio transport by @kimata1007 in https://github.com/kimata1007/codex-runner-controller/pull/11
- feat: add app server session lifecycle by @kimata1007 in https://github.com/kimata1007/codex-runner-controller/pull/12
- feat: add runner agent http api by @kimata1007 in https://github.com/kimata1007/codex-runner-controller/pull/13
- feat: persist runs and app server events by @kimata1007 in https://github.com/kimata1007/codex-runner-controller/pull/14
- feat: add runner agent http client by @kimata1007 in https://github.com/kimata1007/codex-runner-controller/pull/16
- feat: dispatch user messages to Codex by @kimata1007 in https://github.com/kimata1007/codex-runner-controller/pull/17
- feat: start runs from message api by @kimata1007 in https://github.com/kimata1007/codex-runner-controller/pull/18
- feat: wire runner api to session agent by @kimata1007 in https://github.com/kimata1007/codex-runner-controller/pull/19
- feat: track active run in runner agent by @kimata1007 in https://github.com/kimata1007/codex-runner-controller/pull/20
- Revert "feat: propagate run id to runner agent" by @kimata1007 in https://github.com/kimata1007/codex-runner-controller/pull/68
- feat: propagate run id to runner agent by @kimata1007 in https://github.com/kimata1007/codex-runner-controller/pull/21
- feat: forward app server events from runner agent by @kimata1007 in https://github.com/kimata1007/codex-runner-controller/pull/22
- refactor: clarify runner API and agent client packages by @kimata1007 in https://github.com/kimata1007/codex-runner-controller/pull/69
- feat: add internal run event ingest api by @kimata1007 in https://github.com/kimata1007/codex-runner-controller/pull/23
- feat: wire durable run events and sse by @kimata1007 in https://github.com/kimata1007/codex-runner-controller/pull/24
- feat: expose session runner through service by @kimata1007 in https://github.com/kimata1007/codex-runner-controller/pull/25
- feat: resolve runner agent per conversation by @kimata1007 in https://github.com/kimata1007/codex-runner-controller/pull/27
- feat: add session activation boundary by @kimata1007 in https://github.com/kimata1007/codex-runner-controller/pull/28
- refactor: separate session activation adapter by @kimata1007 in https://github.com/kimata1007/codex-runner-controller/pull/29
- feat: suspend idle session pods by @kimata1007 in https://github.com/kimata1007/codex-runner-controller/pull/30
- feat: protect runner http APIs with bearer tokens by @kimata1007 in https://github.com/kimata1007/codex-runner-controller/pull/31
- chore: add controller kubernetes manifests by @kimata1007 in https://github.com/kimata1007/codex-runner-controller/pull/32
- build: add service container images by @kimata1007 in https://github.com/kimata1007/codex-runner-controller/pull/33
- ci: add go and manifest validation by @kimata1007 in https://github.com/kimata1007/codex-runner-controller/pull/34
- feat: add text-first pwa client by @kimata1007 in https://github.com/kimata1007/codex-runner-controller/pull/35
- feat: configure runner pod runtime wiring by @kimata1007 in https://github.com/kimata1007/codex-runner-controller/pull/36
- feat: add api and libsql platform manifests by @kimata1007 in https://github.com/kimata1007/codex-runner-controller/pull/37
- feat: add PWA deployment manifest by @kimata1007 in https://github.com/kimata1007/codex-runner-controller/pull/38
- fix: expose runner agent readiness probe by @kimata1007 in https://github.com/kimata1007/codex-runner-controller/pull/39
- fix: make message dispatch idempotent by @kimata1007 in https://github.com/kimata1007/codex-runner-controller/pull/40
- feat: persist conversation repository configuration by @kimata1007 in https://github.com/kimata1007/codex-runner-controller/pull/41
- feat: project app server events to run status by @kimata1007 in https://github.com/kimata1007/codex-runner-controller/pull/70
- feat: handle app server initiated rpc requests by @kimata1007 in https://github.com/kimata1007/codex-runner-controller/pull/43
- feat: add runner agent approval handling by @kimata1007 in https://github.com/kimata1007/codex-runner-controller/pull/44
- feat: proxy approvals through runner api by @kimata1007 in https://github.com/kimata1007/codex-runner-controller/pull/45
- feat: add approval controls to pwa by @kimata1007 in https://github.com/kimata1007/codex-runner-controller/pull/46
- feat: restore pwa conversations and modes by @kimata1007 in https://github.com/kimata1007/codex-runner-controller/pull/47
- feat: bootstrap repository workspaces in runner pods by @kimata1007 in https://github.com/kimata1007/codex-runner-controller/pull/48
- test: cover runner agent event authentication by @kimata1007 in https://github.com/kimata1007/codex-runner-controller/pull/49
- refactor: centralize default session idle timeout by @kimata1007 in https://github.com/kimata1007/codex-runner-controller/pull/50
- feat: configure default git credentials secret by @kimata1007 in https://github.com/kimata1007/codex-runner-controller/pull/51
- fix: checkout repository revisions after cloning by @kimata1007 in https://github.com/kimata1007/codex-runner-controller/pull/52
- docs: add deployment quickstart by @kimata1007 in https://github.com/kimata1007/codex-runner-controller/pull/53
- fix: bound session readiness wait by @kimata1007 in https://github.com/kimata1007/codex-runner-controller/pull/54
- fix: resume the latest conversation thread by @kimata1007 in https://github.com/kimata1007/codex-runner-controller/pull/55
- feat: keep active session pods alive with heartbeats by @kimata1007 in https://github.com/kimata1007/codex-runner-controller/pull/56
- feat: persist completed assistant messages by @kimata1007 in https://github.com/kimata1007/codex-runner-controller/pull/57
- feat: persist approval lifecycle by @kimata1007 in https://github.com/kimata1007/codex-runner-controller/pull/58
- feat: project session execution phases by @kimata1007 in https://github.com/kimata1007/codex-runner-controller/pull/59
- fix: create the codex workspace for chat sessions by @kimata1007 in https://github.com/kimata1007/codex-runner-controller/pull/60
- fix: stop heartbeats after runs finish by @kimata1007 in https://github.com/kimata1007/codex-runner-controller/pull/61
- docs: align implementation guides with runtime behavior by @kimata1007 in https://github.com/kimata1007/codex-runner-controller/pull/62
- feat: inject Codex App Server authentication by @kimata1007 in https://github.com/kimata1007/codex-runner-controller/pull/63
- ci: build and publish versioned images with tagpr by @kimata1007 in https://github.com/kimata1007/codex-runner-controller/pull/72
