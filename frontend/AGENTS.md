## デザイン方針

**コンセプト**: ニュートラル・プロダクト基調。重要情報を正しくスムーズに認識できることを最優先とし、装飾は最小限。遊びすぎない。

### スマホ優先
- 想定主要環境はスマホ。1 カラム + 縦スクロール前提
- ページコンテンツは `max-w-2xl` で中央寄せ（PC でも膨張させない）
- タップ可能要素は最低 `h-11`（44px）。本文は `text-base`（16px）を基準

### カラー
- ベース: `zinc-50` 背景 / `zinc-900` 文字 / `zinc-200` ボーダー
- アクセント: `emerald-700`（プライマリボタン、日付など主役色）
- 警告: `amber`（未精算アラート、締切表示）
- ステータス: 精算済み = `zinc-100/600`（控えめな灰色）、未精算 = `amber-50/800`
- 解決 / 失敗のような成否は **管理しない**（仕様）

### タイポグラフィ
- 本文: Noto Sans JP（`next/font/google`、`preload: false`）。Tailwind の既定フォント（`--font-sans`）に割り当て済み
- 数字・日付・金額: Geist Mono（`--font-mono`）+ `tabular-nums` で桁ズレ防止

### 共通 UI 部品
`app/_components.tsx` に共通部品を集約：

| 部品 | 用途 |
|---|---|
| `AppHeader` / `PageShell` / `Section` / `SectionTitle` | レイアウト骨格 |
| `PrimaryButton` | 深緑塗りボタン |
| `Badge`（`settled` / `unsettled` / `muted`） | ステータス表示 |
| `Mono` | 数字・日付用の等幅 + tabular-nums |
| `TicketCard` / `EventCover` | チケット 1 枚分のカード / 公演の OG 画像 |
| `UnsettledBanner` | 未精算・未回収があるときのダッシュボードへの導線 |

### Tailwind 運用
- スタイルの再利用は **React コンポーネント抽出を第一手段**にする（`@apply` でクラスを増やす方針は取らない）
- ブランドカラーをトークン化したくなったら `globals.css` の `@theme` ブロックに足す（現状は zinc / emerald / amber で足りているので未導入）

### フォント読み込みのスコープ
全ページ共通のフォント・背景色・文字色はルートの `app/layout.tsx` と `globals.css` で 1 度だけ設定する。ページグループごとの `layout.tsx` に同じ設定を複製しない。

<!-- BEGIN:nextjs-agent-rules -->
# This is NOT the Next.js you know

This version has breaking changes — APIs, conventions, and file structure may all differ from your training data. Read the relevant guide in `node_modules/next/dist/docs/` before writing any code. Heed deprecation notices.
<!-- END:nextjs-agent-rules -->
