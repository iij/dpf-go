# Phase 1 Data Model: 憲章準拠のツール整備

**Date**: 2026-09-09
**Spec**: [spec.md](./spec.md) / **Research**: [research.md](./research.md)

本機能が扱うのは永続データではなく、検査の入力・中間表現・判定結果である。
以下は仕様の Key Entities を実装可能な形に落としたものである。

---

## 1. 検査対象モジュール (Module)

リポジトリ内の 1 つの Go モジュール。`go.mod` の位置から導出する（FR-027）。

| フィールド | 型 | 説明 |
|---|---|---|
| `Path` | string | リポジトリルートからの相対パス（`.`, `misc/vault` など） |
| `ModulePath` | string | `go.mod` の `module` 行の値（`github.com/iij/dpf-go` など） |
| `IsRoot` | bool | 本体モジュールか。FR-012 の判定に用いる |

**導出規則**:

- リポジトリ内の `go.mod` を探索して得る。`vendor/` と `testdata/` 配下は除外する。
- 固定の列挙を持たない（FR-027）。現在は 5 件が得られる。
- `IsRoot` は `Path == "."` で決まる。

---

## 2. 許容ライセンスリスト (Allowlist)

依存に許してよいライセンスの集合。リポジトリ内の単一のファイルで定義する（FR-013）。
出典は憲章の「ライセンス」ブロックの表であり、そこと一対一で対応する。

| フィールド | 型 | 説明 |
|---|---|---|
| `ID` | string | SPDX ライセンス識別子 |
| `Obligation` | enum | `notice`（表示のみ） / `reciprocal`（ソース提供義務を伴う） |

**現在の値**:

| ID | Obligation |
|---|---|
| `Apache-2.0` | `notice` |
| `MIT` | `notice` |
| `BSD-2-Clause` | `notice` |
| `BSD-3-Clause` | `notice` |
| `ISC` | `notice` |
| `MPL-2.0` | `reciprocal` |

**検証規則**:

- ここに無い識別子は許容しない（FR-009）。
- `reciprocal` の依存は許容するが区別して報告する（FR-011）。
- `reciprocal` の依存が `IsRoot` なモジュールに現れた場合は失敗とする（FR-012）。
- リストへの追加は憲章の改訂を伴う（憲章「許容リストにないライセンスの依存が
  必要になった場合、追加する前に本 constitution を改訂すること」）。検査ツールは
  リストの内容を判断せず、リストとの一致だけを見る。

---

## 3. 依存レコード (Dependency)

ある `Module` が実際に import している 1 つの第三者パッケージ。

| フィールド | 型 | 説明 |
|---|---|---|
| `OwnerModule` | Module | この依存を持つモジュール |
| `PackagePath` | string | パッケージパス（`github.com/hashicorp/vault/api` など） |
| `LicenseURL` | string | ライセンス本文の位置 |
| `LicenseID` | string | 判定された SPDX 識別子。判別不能は `Unknown` |

**収集規則**:

- 母集団は「実際に import されるパッケージ」とする（D2）。モジュールグラフ全体は使わない。
- 収集はモジュールごとに行う。これにより `OwnerModule` が定まり FR-012 を実装できる。
- 間接依存（依存の依存）も、import されている限り含まれる（FR-007）。

---

## 4. ライセンス判定 (Verdict)

`Dependency` と `Allowlist` から導かれる判定。1 つの依存に 1 つ。

| 値 | 条件 | ゲートへの影響 |
|---|---|---|
| `allowed` | `LicenseID` が `notice` として許容リストにある | 通す |
| `allowed_reciprocal` | `LicenseID` が `reciprocal` として許容リストにあり、`OwnerModule` が本体でない | 通す。義務を伴う依存として報告する（FR-011） |
| `violation_root_reciprocal` | `LicenseID` が `reciprocal` で `OwnerModule` が本体 | 失敗（FR-012） |
| `violation_disallowed` | `LicenseID` が許容リストに無い | 失敗（FR-009） |
| `violation_undetermined` | `LicenseID` が `Unknown` | 失敗（FR-009）。「許容」として扱わない |

**判定の順序**: `violation_undetermined` → `violation_disallowed` →
`violation_root_reciprocal` → `allowed_reciprocal` → `allowed`。
先に一致したものを採る。判別不能を最優先で見るのは、判別できないものを
許容側へ倒さないためである。

---

## 5. ファイル冒頭規約レコード (FileHeader)

リポジトリ内の 1 つの Go ファイルについて、冒頭の規約の充足状況。

| フィールド | 型 | 説明 |
|---|---|---|
| `Path` | string | リポジトリルートからの相対パス |
| `HasSPDX` | bool | 先頭行が `// SPDX-License-Identifier: Apache-2.0` か（FR-001） |
| `PreamblePragma` | bool | `package` 宣言より前に `nolint` 指示があるか（FR-018） |

**検査規則**:

- 対象はリポジトリ内のすべての `.go` ファイル。テストファイル、ビルドタグ付き
  ファイル、生成物を含む（FR-002）。すべてのモジュールを横断する（FR-003）。
- 除外は `vendor/` と `testdata/` のみ。生成物を除外しない（生成物にも憲章が
  ヘッダを要求している）。
- `HasSPDX` は**先頭行**で判定する。2 行目以降にあっても不足とみなす。
  憲章が「ファイルの先頭に」と定めているため。
- 違反したファイルはすべて列挙する。最初の 1 件で打ち切らない（FR-004）。

---

## 6. 脆弱性報告 (VulnFinding)

到達可能な既知脆弱性の 1 件。

| フィールド | 型 | 説明 |
|---|---|---|
| `ID` | string | 脆弱性の識別子（`GO-YYYY-NNNN` など） |
| `Module` | string | 影響を受ける依存 |
| `Trace` | string | 到達経路（FR-023） |
| `OwnerModule` | Module | どのモジュールの検査で出たか |

**扱い**:

- 到達しないものは報告に含めない（FR-020、US4 シナリオ 3）。
- **1 件でも報告があれば統合できない**（FR-021）。リリースは統合済みの状態から行うため、
  リリース専用の判定は無い。
- 報告を抑制する手段、迂回する手段を設けない（FR-022）。止まった場合の対処は
  依存の更新、または脆弱な経路へ到達しない形への修正である。

---

## 7. 固定バージョン (PinnedTool)

外部の検査ツールと、その固定された版。定義はリポジトリ内の 1 か所（Makefile）に置く（D6）。

| フィールド | 型 | 説明 |
|---|---|---|
| `Name` | string | ツール名 |
| `Version` | string | 固定する版 |
| `ProbeCommand` | string | 導入済みの版を取得するコマンド。ツールごとに異なる |

**現在の値**:

| Name | Version | ProbeCommand |
|---|---|---|
| `golangci-lint` | `2.13.2` | `golangci-lint --version` |
| `govulncheck` | `v1.7.0` | `govulncheck --version` |
| `go-licenses` | `v2.0.1` | `go version -m $(command -v go-licenses)` |
| `betterleaks` | `v1.8.1` | `go version -m $(command -v betterleaks)` |

`go-licenses` は `--version` / `-version` / `version` のいずれも持たないため、
ビルド情報から読む。`betterleaks` は `--version` を持つが `dev` を返すため、
同じくビルド情報から読む（research.md D9）。

`ProbeCommand` は `make` のレシピでコマンド位置へ直接展開する。シェル変数へ
代入してはならない（research.md D11）。

**検証規則**:

- ローカルの検査は、`ProbeCommand` の出力から版を抽出し、`Version` と一致することを
  確かめる。一致しない、未導入、または**抽出に失敗した**場合は中断する（FR-025）。
  いずれも成功扱いにしてはならない。
- 一括実行（`make check`）は 4 件すべての検証をゲート実行の前にまとめて行う。
  これにより「検査できなかった」と「違反があった」が実行順序で区別される
  （research.md D10）。
- CI は自前で版を持たず、この定義を参照する（FR-015、FR-029）。
