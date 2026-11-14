# Codex Runner interface

## Product

Codex Runner is a mobile-first chat surface for starting and resuming remote coding work. The primary job is to continue a conversation and understand when Codex needs input. It is not an operations dashboard.

## Visual direction

Use a quiet, precise interface with a warm-neutral canvas and graphite text. The distinctive detail is the muted sage selection color for the active conversation. Keep all other surfaces and controls restrained so the transcript stays in focus.

Do not use a uniform blue accent for unrelated actions. The “New chat” action is outlined because it navigates to an empty conversation. The send button is graphite because it submits the current message. The composer plus control is neutral because it opens optional context. Use amber only for approval requests, where it signals a decision that needs attention.

Use Liquid Glass only when translucency meaningfully supports a platform navigation surface. The transcript, composer, repository picker, and approval request need stable contrast and do not benefit from blur or refraction.

## Tokens

| Token | Value | Use |
| --- | --- | --- |
| Canvas | `#f7f7f5` | App background |
| Sidebar | `#f1f2ef` | History navigation |
| Surface | `#ffffff` | Conversation and composer |
| Text | `#242622` | Main content |
| Secondary text | `#6c7068` | Status and supporting labels |
| Divider | `#dedfda` | Structural separation |
| Selected row | `#e3e8e0` | Current conversation |
| Primary action | `#292c27` | Send and confirm |
| Focus | `#677a61` | Keyboard focus |
| Approval | `#8a5b18` | Approval affordance only |

## Typography

- Use the platform system sans stack so Japanese glyphs render with the native system font.
- Conversation body: 17px desktop and 16px compact layouts, with 1.7 line-height.
- Composer: 16px with a 24px line-height.
- Conversation title: 16px semibold; history rows: 14px; supporting state: 12–13px.
- Use size, weight, and spacing to express hierarchy. Avoid decorative display faces, all-caps labels, and light weights at small sizes.

## Layout and components

- Desktop: persistent 272px history sidebar; centered transcript with a maximum reading width around 760px; composer anchored below it.
- Compact screens: move history into a modal drawer and keep the transcript and composer in one column.
- Do not wrap every assistant response in a card. The assistant message is the reading surface; use a subtle background only for the user's message.
- Keep the composer as one outlined surface. The plus button opens repository choices; the repository is shown as a removable context chip before the first message.
- Approval is a card because it groups a consequential decision. Show a short summary, a collapsed detail disclosure, and two explicit actions.
- Prefer direct labels and ordinary buttons over icon-only actions. Icon-only controls require accessible names and visible focus.

## Interaction

- Create the conversation on first send, then add it to history using the first user message as its title.
- Repository selection is optional context attached through the composer plus button; a chat without a repository is a normal chat.
- Use concise, truthful status text. Use color only where it communicates selection, progress, warning, or error.
- Support keyboard operation, visible focus, reduced motion, safe areas, and text resizing without clipping.
