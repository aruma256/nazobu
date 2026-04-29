@AGENTS.md

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
- 本文: Noto Sans JP（`next/font/google`、`preload: false`）
- 数字・日付・金額: Geist Mono（既存 `--font-mono`）+ `tabular-nums` で桁ズレ防止

### 共通 UI 部品
`app/mypage/_components.tsx` にマイページ用部品を集約：

| 部品 | 用途 |
|---|---|
| `AppHeader` / `PageShell` / `Section` / `SectionTitle` | レイアウト骨格 |
| `ListCard` | 角丸・薄ボーダーの白カード（`divide-y` 内蔵） |
| `AlertCard` / `AlertItem` | 琥珀ベースの警告ブロック |
| `PrimaryButton` | 深緑塗りボタン |
| `Badge`（`settled` / `unsettled` / `muted`） | ステータス表示 |
| `Mono` | 数字・日付用の等幅 + tabular-nums |

他ページから使うようになったら `app/_components/` 等に昇格する。

### Tailwind 運用
- スタイルの再利用は **React コンポーネント抽出を第一手段**にする（`@apply` でクラスを増やす方針は取らない）
- ブランドカラーをトークン化したくなったら `globals.css` の `@theme` ブロックに足す（現状は zinc / emerald / amber で足りているので未導入）

### フォント読み込みのスコープ
ページグループ単位の `layout.tsx` で `next/font/google` を読み込む。ルートの `app/layout.tsx` には触らない（影響範囲が広すぎるため）。
