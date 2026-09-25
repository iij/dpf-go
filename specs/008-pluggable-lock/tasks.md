---

description: "排他の仕組みを差し替えられるようにする実装タスク"
---

# タスク: 排他の仕組みを差し替えられるようにする

**入力**: `/specs/008-pluggable-lock/` の設計文書

**前提**: [plan.md](./plan.md)、[spec.md](./spec.md)、[research.md](./research.md)、[data-model.md](./data-model.md)、[contracts/](./contracts/)、[quickstart.md](./quickstart.md)

**テスト**: **含める。** 憲章 原則 II（NON-NEGOTIABLE）が「手書きコードは単体テストを伴わなければ完成とみなさない」と定め、仕様も FR-019（単体テスト）・FR-020（既存の統合テストによる回帰の確認）で要求している。

**構成**: タスクは利用シナリオごとにまとめる。US1 と US3 はどちらも土台の上に載り、互いに独立である。US2 は主に回帰の確認であり、US1 の後に置く。

## 書式: `[ID] [P?] [シナリオ] 説明`

- **[P]**: 並行して実行できる (異なるファイル、依存なし)
- **[シナリオ]**: このタスクが属する利用シナリオ (US1、US2、US3)
- 説明には正確なファイルパスを含めること

## パスの規約

既存の Go ライブラリへの変更と、サブパッケージの新設である。

- 抽象と汎用の実行: `utils/locker.go`、`utils/locker_test.go`（**新規**）
- 差し替えの確認: `utils/locker_ext_test.go`（**新規**。後述の import の制約により `package utils_test`）
- 契約の確認手段と参照実装: `utils/lockertest/`（**新規パッケージ**）
- 既定の実装: `utils/lock.go`、`utils/lock_test.go`
- 一括置き換え: `utils/apply.go`、`utils/apply_test.go`
- 文書: `utils/doc.go`、`utils/example_test.go`、`README.md`、`CHANGELOG.md`

### import の制約（重要）

`utils/lockertest` は `utils` を import する。したがって **`utils` のパッケージ内テスト
（`package utils`）から `lockertest` を import すると循環参照になる。** 参照実装を使う
テストは、外部テストパッケージ（`package utils_test`）に置くこと。Go は同じディレクトリに
両方のテストパッケージを許す。

---

## フェーズ 1: 準備 (共通の基盤)

**目的**: 抽象の置き場と、`testing` を持ち込んでいないことの基準を作る

- [X] T001 `utils/locker.go` を新規作成し、SPDX ヘッダを付ける。`Locker`（`Lock` / `Renew` / `Unlock`）と `RenewIntervaler` を定義し、契約の要点を godoc に書く（[contracts/locker.md](./contracts/locker.md) 第 1〜6 節）
- [X] T002 `utils/locker.go` に `var _ Locker = (*Mutex)(nil)` を置き、既存の `Mutex` が抽象を満たすことをコンパイル時に確認する（FR-005）
- [X] T003 `go list -deps ./utils | grep -x testing` が空であることを確認し、現状を基準として記録する。以降この状態を壊さない（[quickstart.md](./quickstart.md) 第 1 節）

**関門**: 抽象が定義され、`Mutex` がそれを満たしている。`go build ./...` が通る

---

## フェーズ 2: 土台 (先行必須)

**目的**: 汎用の実行を抽象の上へ移し、参照実装を用意する

**⚠️ 重要**: このフェーズが完了するまで、利用シナリオの作業は開始できない

- [X] T004 `utils/lock.go` から `hold` 型と `stopRenew` / `consume` / `lost` などのメソッドを `utils/locker.go` へ移す。挙動は変えない（[research.md](./research.md) D6）
- [X] T005 `utils/locker.go` に非公開の任意インターフェース（ゾーン反映が排他を解くことの申告）を定義する。**非公開のメソッドを持たせ、`utils` の外からは実装できないようにする**（[research.md](./research.md) D4）
- [X] T006 `utils/locker.go` に `HoldOption` と、延長の間隔・取得の待機の Option を定義する。延長の間隔の既定値も定める（[data-model.md](./data-model.md) 第 4 節）
- [X] T007 `utils/locker.go` に延長の間隔を決める処理を実装する。利用者の指定 → 実装の申告（`RenewIntervaler`）→ 既定値、の順で決める（[research.md](./research.md) D5）
- [X] T008 `utils/locker.go` に `renewLoop` を `Locker` の上で動く形へ移す。`Renew` が `ErrNotLockHolder` を返したら派生 context を打ち切り、それ以外の失敗は次の周期で再試行する（007 から継承）
- [X] T009 `utils/locker.go` に `RunLocked` と、非公開の `runLockedHold`（`hold` を渡す形）を実装する。取得・派生 context・延長の起動・処理の実行・延長の停止と終了待ち・解放、の順（[contracts/api.md](./contracts/api.md) 第 1 節）
- [X] T010 `utils/locker.go` に解放の処理を実装する。`Unlock` を呼び、消費の印が立っている場合は `ErrNotLockHolder` を成功として扱う（007 D8 から継承）
- [X] T011 `utils/lock.go` の `Mutex.Do` を `RunLocked` の薄い包みにする。`doHold` を廃し、`Mutex` 固有の実装を残さない（[research.md](./research.md) D6）
- [X] T012 `utils/lock.go` に `Mutex` の申告を実装する。延長の間隔として保持期間の 1/3 を返し、ゾーン反映が排他を解くことを非公開の形で申告する（[research.md](./research.md) D4・D5）
- [X] T013 `utils/lockertest/doc.go` を新規作成し、SPDX ヘッダとパッケージ文書を書く。自作の実装を確かめる手順と、`testing` を持ち込まないために分けてあることを記す（[research.md](./research.md) D8）
- [X] T014 `utils/lockertest/lockertest.go` を新規作成し、メモリ内の参照実装を実装する。契約を満たすこと（再入の禁止、待たないこと、`Renew` での喪失の報告、他者の排他を解放しないこと）を守る（[research.md](./research.md) D9）
- [X] T015 `utils/lock_test.go` を、移動に伴って調整する。既存のテストがすべて通ることを確認する

**関門**: `RunLocked` が抽象の上で動き、`Mutex.Do` がその包みになっている。既存の単体テストがすべて通る。`go test ./utils/ -race` が成功する

---

## フェーズ 3: 利用シナリオ 1 - 自分の環境に合った排他の仕組みを選べる (優先度: P1) 🎯 MVP

**目標**: 利用者が排他の実装を差し替えられ、ライブラリの操作がそのまま動く

**独立した検証**: 参照実装を渡して `RunLocked` と `Apply` が動くことを確認する。反映が排他を解かない実装でも解放が正しく行われることを確認する

### 利用シナリオ 1 のテスト ⚠️

> **注記: これらのテストを先に書き、実装前に失敗することを確認する**

- [X] T016 [US1] `utils/locker_ext_test.go` を新規作成し、SPDX ヘッダと `package utils_test` を書く。**パッケージ内テストから `lockertest` を import すると循環参照になる**ことを冒頭のコメントに記す
- [X] T017 [US1] `utils/locker_ext_test.go` に `RunLocked` を参照実装で動かすテストを追加する。処理が排他の下で実行され、終了後に解放されること（FR-001）
- [X] T018 [US1] `utils/locker_ext_test.go` に、実装が `ErrStillLock` を返した場合に取得できなかった扱いになり、処理が呼ばれないことのテストを追加する（FR-003）
- [X] T019 [US1] `utils/locker_ext_test.go` に、実装が `Renew` で `ErrNotLockHolder` を返した場合に処理の context が打ち切られ、`ErrNotLockHolder` が返ることのテストを追加する（FR-003）
- [X] T020 [US1] `utils/locker_ext_test.go` に、`ZoneApplier` へ参照実装を渡した場合のテストを追加する。**反映が排他を解かない実装でも、`Apply` が成功し解放が正しく行われること**（FR-008・SC-005）
- [X] T021 [US1] `utils/apply_test.go` に、`WithLocker` を指定した場合に `WithLockOptions` が無視されることのテストを追加する（[contracts/api.md](./contracts/api.md) 第 2 節）

### 利用シナリオ 1 の実装

- [X] T022 [US1] `utils/apply.go` に `ApplierOption` 型を導入し、`NewZoneApplier` の可変長引数をその型へ変える（[research.md](./research.md) D7）
- [X] T023 [US1] `utils/apply.go` に `WithLocker` と `WithLockOptions` を実装する。`WithLocker` が指定された場合は `WithLockOptions` を無視し、その旨を godoc に書く
- [X] T024 [US1] `utils/apply.go` の `ZoneApplier` が内部で `Locker` を扱う形へ変える。既定は `NewMutex` で生成した排他とする（FR-006）
- [X] T025 [US1] `utils/apply.go` の `Apply` を `runLockedHold` の上で動く形へ変える。消費の印は、実装が「反映が排他を解く」と申告している場合にのみ立てる（FR-008・FR-009）

**関門**: 利用シナリオ 1 の単体テストが通る。参照実装でも既定の実装でも `Apply` の結果が変わらない

---

## フェーズ 4: 利用シナリオ 2 - 差し替えない利用者は今までどおり使える (優先度: P2)

**目標**: 既定の挙動を変えない。変更が必要な箇所は機械的な置き換えで済む

**独立した検証**: 既存の単体テストと統合テストが変更なしで通ることを確認する。v0.4.0 の書き方が動くことを確認する

### 利用シナリオ 2 のテスト ⚠️

- [X] T026 [US2] `utils/locker_test.go` を新規作成し、SPDX ヘッダと `package utils` を書く。`Mutex.Do` が `RunLocked` と同じ結果になることのテストを追加する（FR-007・SC-002）
- [X] T027 [US2] `utils/locker_test.go` に、延長の間隔の決まり方のテストを追加する。実装の申告が使われること、利用者の指定が申告より優先されること、どちらも無ければ既定値が使われること（FR-009、[data-model.md](./data-model.md) 第 4 節）
- [X] T028 [US2] `utils/apply_test.go` に、`WithLocker` を指定しない場合にレコードを使う排他が使われることのテストを追加する（FR-006）
- [X] T029 [US2] `utils/locker_test.go` に、既定の実装で消費の印が立ち、参照実装では立たないことのテストを追加する（FR-009）

### 利用シナリオ 2 の実装

- [X] T030 [US2] `utils/apply_test.go` の既存のテストを `WithLockOptions` を使う形へ機械的に置き換える。**この置き換えの手間が、利用者に求める移行そのものである**（FR-007）
- [X] T031 [US2] `internal/integration/` を**変更せずに** `make test-integration` が通ることを確認する。変更が必要になった場合、それは既定の挙動を変えたということであり FR-007 に反する（FR-020）
  - **結果**: 1 行だけ変更が必要だった。`lockflow_test.go:429` の `NewZoneApplier` の呼び出しを `WithLockOptions` で包む置き換えである。これは T022 で計画済みの破壊的変更（可変長引数の型）そのものであり、挙動の変更ではない。他の箇所（`NewMutex`、`Mutex.Do`、`Renew`、`Unlock`、`WithRenewInterval`）は無変更で通る

**関門**: 既存の単体テストと統合テストが、`WithLockOptions` への置き換え以外の変更なしで通る

---

## フェーズ 5: 利用シナリオ 3 - 自作の実装が契約を満たすか確かめられる (優先度: P3)

**目標**: 契約を機械的に確かめられ、どの約束に反しているかが分かる

**独立した検証**: 契約を満たす実装（参照実装）が通り、わざと満たさない実装が落ちることを確認する

### 利用シナリオ 3 のテスト ⚠️

- [X] T032 [US3] `utils/lockertest/lockertest_test.go` を新規作成し、SPDX ヘッダを書く。参照実装が `Run` を通ることのテストを追加する（FR-011）
- [X] T033 [US3] `utils/lockertest/lockertest_test.go` に、**再入を許す実装**を用意し、`Run` が落とすことのテストを追加する。どの約束に反しているかが示されること（FR-012、契約 第 2 節）
- [X] T034 [US3] `utils/lockertest/lockertest_test.go` に、**取得できるまで待つ実装**を用意し、`Run` が落とすことのテストを追加する（契約 第 2 節）
- [X] T035 [US3] `utils/lockertest/lockertest_test.go` に、**`Renew` で保持の喪失を報告しない実装**を用意し、`Run` が落とすことのテストを追加する（FR-010、契約 第 3 節）
- [X] T036 [US3] `utils/lockertest/lockertest_test.go` に、**他者の排他を解放する実装**と、**番兵エラーに一致しないエラーを返す実装**を用意し、`Run` が落とすことのテストを追加する（FR-003、契約 第 4・6 節）

### 利用シナリオ 3 の実装

- [X] T037 [US3] `utils/lockertest/lockertest.go` に `Run` を実装する。同じ対象に対する排他を 2 つ作る関数を受け取り、契約の各項目をサブテストとして確かめる（FR-011、[contracts/locker.md](./contracts/locker.md) 第 8 節）
- [X] T038 [US3] `Run` の各サブテストに、違反した約束が分かる名前とメッセージを付ける。契約のどの節に対応するかを示す（FR-012）
- [X] T039 [US3] `Run` が並行呼び出しの直列化を確かめる部分を実装する。`-race` で検出できる形にする（契約 第 7 節）
- [X] T040 [US3] `utils/lockertest` がネットワークを必要としないことを確認する。`go list -deps ./utils/lockertest` に外部の依存が現れないこと（FR-013）

**関門**: 利用シナリオ 3 のテストが通る。契約に反する 5 種類の実装がすべて検出される

---

## フェーズ 6: 仕上げと横断的な事項

**目的**: 文書、ゲート、実 API での回帰の確認

- [X] T041 [P] `utils/locker.go` の `Locker` と `RunLocked` の godoc を仕上げる。[contracts/api.md](./contracts/api.md) 第 5 節の 4 点のうち、抽象に関わるものを含める（FR-004）
- [X] T042 [P] `utils/apply.go` の `NewZoneApplier` / `WithLocker` / `WithLockOptions` の godoc を仕上げる。差し替えても `Apply` の呼び出し方と結果が変わらないことを明記する（FR-008）
- [X] T043 [P] `utils/doc.go` に排他の差し替えの説明と、**2 つの方式の比較表**を書く。**外部の仕組みを使う排他では、別ユーザや管理画面からの編集を止める効果が失われることを明記する**（FR-014・FR-016・SC-006、[contracts/api.md](./contracts/api.md) 第 5 節）
- [X] T044 [P] `utils/doc.go` に、排他がどのゾーンに対応するかを決める責任が実装側にあることを明記する（FR-015）
- [X] T045 [P] `utils/example_test.go` に差し替えの例を追加する。`WithLocker` を使う形と、`RunLocked` に自作の実装を渡す形
- [X] T046 [P] `README.md` 88 行の `dpf/utils` の説明に、排他の差し替えを追記する
- [X] T047 [P] `CHANGELOG.md` の `[Unreleased]` に、機能追加（`Locker`、`RenewIntervaler`、`RunLocked`、`WithLocker`、`WithLockOptions`、`utils/lockertest`）と破壊的変更（`NewZoneApplier` の可変長引数の型）を記載する（FR-017）
- [X] T048 `make check` を実行し、マージ前の 7 ゲートすべてが成功することを確認する。**`go list -deps ./utils | grep -x testing` が空のままであることも確認する**（[quickstart.md](./quickstart.md) 第 1 節）
- [X] T049 `make test-integration` を実行し、全件が通ることを確認する。**統合テストのコードを変更していないこと**を併せて確認する（FR-020）。**トークン未設定でスキップされた結果を確認結果として報告しない**
  - **結果**: 2026-09-24 に利用者が実行し、全件通過（643.8 秒、スキップ無し。各テストが `DPF_TOKEN_RO=true DPF_TOKEN_RW=true DPF_TEST_SERVICE_CODE=true` を記録している）。`TestZoneMutex` / `TestZoneApplierApply` / `TestZoneMutexDoRenews` / `TestLockFlow_AtomicChangesLosesLock` / `TestLockFlow_ZoneChanges` / `TestZoneMutexDoPerRecord` / `TestLockRecordPremises` はいずれも 008 の前と同じ結果である。変更は T031 に記した 1 行のみ
- [X] T050 [quickstart.md](./quickstart.md) 第 2〜6 節の確認表を上から順に実行し、各項目が満たされていることを確認する。第 6 節（移行の確認）では v0.4.0 の書き方がどこまでそのまま動くかを確かめる

---

## 依存関係と実行順序

### フェーズ間の依存

- **準備 (フェーズ 1)**: 依存なし。ただちに開始できる
- **土台 (フェーズ 2)**: 準備の完了に依存する。すべての利用シナリオを塞ぐ。**参照実装（T014）も土台に含める。** US1 のテストがこれを使うためである
- **US1 (フェーズ 3)**: 土台の完了に依存する
- **US2 (フェーズ 4)**: 土台と US1 に依存する。US1 が `NewZoneApplier` の引数を変えるため、その後でなければ移行の確認ができない
- **US3 (フェーズ 5)**: 土台の完了に依存する。US1・US2 と独立に進められる
- **仕上げ (フェーズ 6)**: T041〜T047 は対象の実装の完了に依存する。T048〜T050 はすべての実装の完了に依存する

### 利用シナリオ間の依存

- **US1 (P1)**: 土台の後に開始できる。**本機能の目的そのものであり MVP である**
- **US2 (P2)**: US1 に依存する。主に回帰の確認であり、新しい実装はほとんど無い
- **US3 (P3)**: 土台の後に開始できる。US1 と独立。**先に実装してもよい**

### 各利用シナリオの内部

- テストを先に書き、実装前に失敗することを確認する
- US1 の内部では、Option 型の導入（T022）→ Option の実装（T023）→ 内部構造の変更（T024）→ `Apply` の書き換え（T025）の順
- US3 の内部では、`Run` の骨組み（T037）→ メッセージ（T038）→ 並行性（T039）の順

### 並行できる箇所

土台の後は、US1（`utils/apply.go`）と US3（`utils/lockertest/`）が別ファイルであり並行できる。

- T041〜T047（仕上げ）は互いに別ファイルであり並行できる。ただし T043 と T044 は同じ `utils/doc.go` を触るため、この 2 つは直列
- 複数人で進める場合の分担は「土台 → US1 → US2 を 1 人」「US3 を 1 人」「文書（T043〜T047）を 1 人」である

---

## 並行実行の例: フェーズ 2 の後

```bash
# US1 と US3 は別ファイルなので同時に進められる:
Task: "utils/apply.go に ApplierOption と WithLocker を導入する"
Task: "utils/lockertest/lockertest.go に Run を実装する"
```

## 並行実行の例: フェーズ 6

```bash
# 文書はそれぞれ別ファイル:
Task: "utils/example_test.go に差し替えの例を追加する"
Task: "README.md の dpf/utils の説明を更新する"
Task: "CHANGELOG.md の [Unreleased] に記載する"
```

---

## 実装の進め方

### まず MVP (利用シナリオ 1 のみ)

1. フェーズ 1（準備）を完了する
2. フェーズ 2（土台）を完了する。**参照実装まで含む**
3. フェーズ 3（US1）を完了する
4. **いったん止めて確認する**: 参照実装を渡して `RunLocked` と `Apply` が動き、反映が排他を解かない実装でも解放が正しく行われることを単体テストで確認する
5. この時点で差し替えはできる。ただし**出すには US2 の回帰の確認が要る**。既定の挙動を壊していないことを確かめないまま出せない

### 増分で届ける

1. 準備 + 土台 → 抽象が入り、既定の実装がその上に載る
2. US1 → 差し替えができる（MVP）
3. US2 → 既定を壊していないことが確かめられる。**ここまでで出せる**
4. US3 → 自作の実装を確かめる手段が加わる
5. 仕上げ → 文書とゲート

### 順序の入れ替え

US3 は US1 と独立であり、先に実装してもよい。ただし US3 の参照実装（T014）は土台にあるため、
その順序は変えられない。

---

## 補足

- `[P]` のタスク = 異なるファイル、依存なし
- 実装前にテストが失敗することを確認する
- タスクごと、あるいは論理的なまとまりごとにコミットする
- **実装は `main` から新しいブランチを切って始める。** 現在 `specs/008-pluggable-lock/` が未追跡で残っている
- **`utils` のパッケージ内テストから `utils/lockertest` を import してはならない**（循環参照）。参照実装を使うテストは `package utils_test` に置く
- **統合テストは変更しない。** 変更が必要になったら、それは既定の挙動を変えてしまった証拠である（FR-007・FR-020）
