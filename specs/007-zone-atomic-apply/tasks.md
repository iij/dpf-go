---

description: "排他を保持したままの実行と、ゾーン全体の一括置き換えの実装タスク"
---

# タスク: 排他を保持したままの実行と、ゾーン全体の一括置き換え

**入力**: `/specs/007-zone-atomic-apply/` の設計文書

**前提**: [plan.md](./plan.md)、[spec.md](./spec.md)、[research.md](./research.md)、[data-model.md](./data-model.md)、[contracts/](./contracts/)、[quickstart.md](./quickstart.md)

**テスト**: **含める。** 憲章 原則 II（NON-NEGOTIABLE）が「手書きコードは単体テストを伴わなければ完成とみなさない」と定め、仕様も FR-027（単体テスト）・FR-028（統合テスト）で要求している。本機能は goroutine を使うため `-race` での検証が要件である。

**構成**: タスクは利用シナリオごとにまとめる。US2 は US1 の上に載るため、US1 の完了に依存する。US3 は US1 と独立に実装できる。

## 書式: `[ID] [P?] [シナリオ] 説明`

- **[P]**: 並行して実行できる (異なるファイル、依存なし)
- **[シナリオ]**: このタスクが属する利用シナリオ (US1、US2、US3)
- 説明には正確なファイルパスを含めること

## パスの規約

既存の Go ライブラリへの機能追加である。新規ファイルは 2 件。

- 本体パッケージ: リポジトリルートの `interfaces.go`
- 汎用の層: `utils/lock.go`、`utils/lock_test.go`
- 一括置き換え: `utils/apply.go`、`utils/apply_test.go`（**新規**）
- 文書: `utils/doc.go`、`utils/example_test.go`、`README.md`、`CHANGELOG.md`
- 実 API の確認: `internal/integration/lockflow_test.go`

---

## フェーズ 1: 準備 (共通の基盤)

**目的**: インターフェースの拡張と、模擬サーバの拡張。両方の利用シナリオが依存する

- [X] T001 [P] `interfaces.go` の `ZonesApi` に `PatchZoneAtomicChanges` を追加し、`JobsApi`（`SyncWaitContext` のみ）を新設する。`var _ ZonesApi = (*ZonesAPIService)(nil)` と `var _ JobsApi = (*JobsAPIService)(nil)` が通ることを確認する（[research.md](./research.md) D10）
- [X] T002 `utils/lock_test.go` の模擬サーバ `lockServer` に `GET /zones/{id}/records/currents` を追加する。反映済み（state=0）のレコードのみを返し、編集予定のレコードについては「更新前の状態」(state=5) として返す（[research.md](./research.md) D1 の実測どおり）
- [X] T003 `utils/lock_test.go` の模擬サーバに `PATCH /zones/{id}/atomic_changes` を追加する。リクエストボディを記録し、テストから `overwrite_soa` と `overwrite_zone_apex_ns` の値を確認できるようにする
- [X] T004 `utils/lock_test.go` の模擬サーバの `atomic_changes` に、実 API の挙動を再現させる。**未反映の編集を破棄し（SOA の編集予定を消し、未反映件数を 0 にする）、リクエストのレコードでゾーンを置き換える。** `overwrite_soa` が false のときは既存の反映済み SOA をラベルごと維持する（[research.md](./research.md) D1・D1b）
- [X] T005 `utils/lock_test.go` の模擬サーバに JOB の状態取得（`GET /jobs/{request_id}`）を追加し、`SyncWaitContext` が完了を待てるようにする

**関門**: 模擬サーバが公開レコードの取得・一括置き換え・JOB の待ち合わせを表現でき、一括置き換えが未反映の編集を破棄する挙動を再現できる

---

## フェーズ 2: 土台 (先行必須)

**目的**: 両方の利用シナリオが使う内部構造と設定

**⚠️ 重要**: このフェーズが完了するまで、利用シナリオの作業は開始できない

- [X] T006 `utils/lock.go` に非公開の `hold` 型を追加する。派生 context、打ち切りの関数、延長の停止の合図、消費の印、延長で失われた旨の記録を持つ（[data-model.md](./data-model.md) 第 1 節）
- [X] T007 `utils/lock.go` の `Mutex` に `renewInterval` と `lockWait` のフィールドを追加し、`renewInterval` の既定値を保持期間の 1/3 として導出する（[research.md](./research.md) D6）
- [X] T008 `utils/lock.go` に Option `WithRenewInterval` を追加する。0 以下は無視して既定のままとする既存の作法に合わせる（FR-003）
- [X] T009 `utils/lock.go` に非公開の `doHold` を追加する。排他の取得、派生 context の作成、利用者の処理の呼び出し、後始末（`Unlock` を呼び、消費の印が立っている場合は `ErrNotLockHolder` を成功として扱う）を行う。**延長はこの段では実装しない**（[research.md](./research.md) D8、[contracts/lifecycle.md](./contracts/lifecycle.md) 第 2 節）
- [X] T010 `utils/lock.go` に公開の `Do` を追加する。`doHold` を呼び、コールバックの引数を context 1 つだけにする（FR-017、[research.md](./research.md) D7）
- [X] T011 `utils/lock_test.go` に土台のテストを追加する。`Do` が排他の下で処理を実行し終了後に解放すること、処理へ渡る context が呼び出し側から派生していること、排他を取得できない場合に処理が呼ばれないこと、処理のエラーがそのまま返ること

**関門**: 延長のない `Do` が動作し、単体テストが通る。`go build ./...` と `make check-lint` が成功する

---

## フェーズ 3: 利用シナリオ 1 - 排他を保持したまま処理を実行できる (優先度: P1) 🎯 MVP

**目標**: 保持期間を自動で延長し、排他を失ったら処理へ通知する

**独立した検証**: 保持期間より長い処理でも保持が続くことを確認する。排他を他者に奪われた状況を作り、処理の context が打ち切られ判別できるエラーが返ることを確認する。`Do` の復帰後に goroutine が残らないことを確認する

### 利用シナリオ 1 のテスト ⚠️

> **注記: これらのテストを先に書き、実装前に失敗することを確認する**

- [X] T012 [US1] `utils/lock_test.go` に自動延長のテストを追加する。保持期間より長い処理の実行中に延長が走り、奪ってよい時刻が進むこと。処理が中断されないこと（FR-002・SC-001）
- [X] T013 [US1] `utils/lock_test.go` に延長の間隔のテストを追加する。既定が保持期間の 1/3 であること。`WithRenewInterval` で変更できること。0 以下は無視されること（FR-003）
- [X] T014 [US1] `utils/lock_test.go` に奪われた場合のテストを追加する。処理の実行中に他者が排他を奪った状況を作り、**処理へ渡した context が打ち切られること**、`ErrNotLockHolder` が返ることを確認する（FR-004・SC-002）
- [X] T015 [US1] `utils/lock_test.go` にエラーの併記のテストを追加する。排他を失い、かつ処理もエラーを返した場合に、`errors.Is` が両方に一致すること（FR-004・FR-008）
- [X] T016 [US1] `utils/lock_test.go` に延長の一時的な失敗のテストを追加する。延長が「保持者でない」以外の理由で失敗した場合、処理が中断されず、次の周期で再試行されること（FR-005）
- [X] T017 [US1] `utils/lock_test.go` に goroutine の後始末のテストを追加する。`Do` の復帰後に延長の goroutine が残っていないことを確認する。`-race` で実行する（[plan.md](./plan.md) 方針 5）
- [X] T018 [US1] `utils/lock_test.go` に後始末の判定のテストを追加する。自分が保持者である場合に限り解放されること。延長が既に「奪われた」を報告している場合、解放時の `ErrNotLockHolder` を重ねて報告しないこと（FR-007、[contracts/lifecycle.md](./contracts/lifecycle.md) 第 2 節）

### 利用シナリオ 1 の実装

- [X] T019 [US1] `utils/lock.go` の `doHold` に延長の goroutine を追加する。`renewInterval` の周期で `Renew` を呼ぶ（[contracts/lifecycle.md](./contracts/lifecycle.md) 第 3 節）
- [X] T020 [US1] `utils/lock.go` の延長の goroutine に失敗の分岐を実装する。`ErrNotLockHolder` は派生 context を打ち切って記録し終了する。それ以外は何もせず次の周期を待つ（FR-004・FR-005、[research.md](./research.md) D5）
- [X] T021 [US1] `utils/lock.go` の `doHold` に goroutine の停止と終了待ちを実装する。`Do` が復帰する前に必ず終了させる（[plan.md](./plan.md) 方針 5）
- [X] T022 [US1] `utils/lock.go` の `doHold` にエラーの組み立てを実装する。延長で失われた旨と処理のエラーの双方がある場合は `errors.Join` で併記する（FR-004・FR-008、[data-model.md](./data-model.md) 第 5 節）

**関門**: 利用シナリオ 1 の単体テストが `-race` で通る。この時点で「排他の下で長い処理を安全に走らせる」ことができ、1 レコードずつ反映する流れにも使える

---

## フェーズ 4: 利用シナリオ 2 - ゾーン全体を読んで編集し、一括で反映できる (優先度: P2)

**目標**: 一括置き換えの 3 つの落とし穴を塞ぐ。利用者が書くのは編集の内容だけ

**独立した検証**: 編集の内容を表す関数を渡し、ゾーンが置き換わることを確認する。2 つのフラグが常に「取り込まない」で送られることを確認する。成功時に解放起因のエラーが返らないことを確認する。受け取った一覧をそのまま返した場合にラベル・TTL・コメントが変わらないことを確認する

### 利用シナリオ 2 のテスト ⚠️

- [X] T023 [US2] `utils/apply_test.go`（新規）を作り、SPDX ヘッダを付ける。基本の流れのテストを追加する。公開レコードが変換されて編集関数へ渡され、その結果で一括置き換えが行われ、反映の完了を待って復帰すること（FR-013・FR-014・FR-019）
- [X] T024 [US2] `utils/apply_test.go` にフラグのテストを追加する。**`overwrite_soa` と `overwrite_zone_apex_ns` が常に false で送られること。** 変更する手段が公開されていないこと（FR-015・FR-016・SC-003）
- [X] T025 [US2] `utils/apply_test.go` に項目の保持のテストを追加する。受け取った一覧をそのまま返した場合、ラベル・TTL・コメントが変わらずに送られること（FR-014b・SC-009）
- [X] T026 [US2] `utils/apply_test.go` に補完のテストを追加する。編集の結果に SOA または Zone Apex の NS が含まれない場合、変換前の一覧のものが補われること（[research.md](./research.md) D9）
- [X] T027 [US2] `utils/apply_test.go` に空の結果のテストを追加する。編集が空を返した場合、一括置き換えの API を呼ばずに `ErrNoRecords` が返り、排他が解放されること（FR-018）
- [X] T028 [US2] `utils/apply_test.go` に成功時の後始末のテストを追加する。**一括置き換えが排他を解いた後に、無条件の解放を行わないこと。** `ErrNotLockHolder` が返らないこと（FR-017・SC-004）
- [X] T029 [US2] `utils/apply_test.go` に失敗時の後始末のテストを追加する。公開レコードの取得、編集、一括置き換え、JOB の待ち合わせのいずれが失敗しても排他が解放されること（SC-005）
- [X] T030 [US2] `utils/apply_test.go` に消費の印の位置のテストを追加する。一括置き換えの実行中に延長の API が呼ばれないことを確認する（[contracts/lifecycle.md](./contracts/lifecycle.md) 第 5 節 第 6 段）
- [X] T031 [US2] `utils/apply_test.go` に Option のテストを追加する。`WithApplyDescription` が反映のコメントに渡ること。排他の Option（保持者・保持期間など）が `NewZoneApplier` から内部の排他へ渡ること（FR-020）

### 利用シナリオ 2 の実装

- [X] T032 [US2] `utils/apply.go`（新規）を作り、SPDX ヘッダとパッケージ内の位置づけを書く。`ZoneRecordsEditor` 型（入出力が同じ型）、`ZoneApplier` 型、`ApplyOption` 型、番兵エラー `ErrNoRecords` を定義する（[research.md](./research.md) D9b・D11、[contracts/api.md](./contracts/api.md) 第 2 節）
- [X] T033 [US2] `utils/apply.go` に `NewZoneApplier` を実装する。`RecordsApi` / `ZonesApi` / `JobsApi` とゾーン ID を受け取り、排他の Option をそのまま内部の `Mutex` へ渡す。**排他を公開しない**（[research.md](./research.md) D2）
- [X] T034 [US2] `utils/apply.go` に `WithApplyDescription` を実装する（FR-020）
- [X] T035 [US2] `utils/apply.go` に公開レコードの取得と変換を実装する。`GetRecordCurrents` の結果を `[]dpf.OverwriteRecordsInner` へ変換する。名前・TTL・RRTYPE・rdata・コメント・ラベルをすべて引き継ぐ（FR-014b、[research.md](./research.md) D9b）
- [X] T036 [US2] `utils/apply.go` に編集の結果の検証と補完を実装する。空なら `ErrNoRecords`。SOA または Zone Apex の NS が無ければ変換前の一覧のものを補う（FR-018、[research.md](./research.md) D9）
- [X] T037 [US2] `utils/lock.go` の `hold` に消費を通知する非公開のメソッドを実装する。延長の goroutine を止め、後始末で `ErrNotLockHolder` を成功として扱うようにする（FR-017、[research.md](./research.md) D7）
- [X] T038 [US2] `utils/apply.go` に `Apply` を実装する。`doHold` の中で、公開レコードの取得 → 編集 → 検証と補完 → **消費の通知** → 一括置き換え（2 つのフラグを false で固定）→ JOB の待ち合わせ、の順に行う（[contracts/lifecycle.md](./contracts/lifecycle.md) 第 5 節）

**関門**: 利用シナリオ 2 の単体テストが通る。一括置き換えの 3 つの落とし穴が構造的に踏めないことが確認できている

---

## フェーズ 5: 利用シナリオ 3 - 排他が取れないときの振る舞いを選べる (優先度: P3)

**目標**: 取得を待つかどうかを選べるようにする

**独立した検証**: 他者が排他を保持している状態で、待たない設定では即座にエラー、待つ設定では取得できるまで繰り返すことを確認する。打ち切りに従うことを確認する

### 利用シナリオ 3 のテスト ⚠️

- [X] T039 [US3] `utils/lock_test.go` に待機のテストを追加する。`WithLockWait` を指定しない場合はただちに `ErrStillLock` が返ること。指定した場合は取得できるまで指定の間隔で繰り返すこと（FR-011）
- [X] T040 [US3] `utils/lock_test.go` に待機の打ち切りのテストを追加する。待機中に呼び出しを打ち切ると、打ち切りを表すエラーが返り、処理が実行されないこと（FR-012・SC-008）

### 利用シナリオ 3 の実装

- [X] T041 [US3] `utils/lock.go` に Option `WithLockWait` を追加する。0 以下は無視して「待たない」とする（FR-011）
- [X] T042 [US3] `utils/lock.go` の `doHold` の排他の取得を、`lockWait` が指定されている場合は `LockWait` を使う形に変える（FR-011・FR-012）

**関門**: 利用シナリオ 3 の単体テストが通る。`Apply` からも待機が指定できる（排他の Option として渡るため）

---

## フェーズ 6: 仕上げと横断的な事項

**目的**: 文書、実 API での確認、既に作業ツリーにある修正の完成

- [X] T043 [P] `utils/lock.go` の `Mutex.Do` に godoc を書く。[contracts/api.md](./contracts/api.md) 第 5 節の 6 点（自動延長、奪われたときの打ち切りと通知であること、**延長が失敗し続ける間は保護が切れた状態で処理が走りうること**、異常終了を捕捉しないこと、1 レコードずつの流れに使えること、ゾーン全体の一括置き換えには `ZoneApplier` を使うこと）を含める（FR-024・FR-025）
- [X] T044 [P] `utils/apply.go` の `ZoneApplier` と `Apply` に godoc を書く。[contracts/api.md](./contracts/api.md) 第 5 節の 5 点を含める（FR-022・FR-023）
- [X] T045 [P] `utils/lock.go` の `Mutex` の godoc を見直す。一括置き換えについて「併用できない」で終わらせず、`ZoneApplier` を案内する形にする（[research.md](./research.md) D12）
- [X] T046 [P] `utils/example_test.go` の `ExampleMutex_atomicChanges` を差し替える。`overwrite_soa=false` なら安全という前提で書いた例は実測と矛盾する。`ZoneApplier` を使う例と、`Mutex.Do` の中で 1 レコードずつ反映する例にする（[research.md](./research.md) D1・D12）
- [X] T047 [P] `utils/doc.go` に 2 つの操作の説明を追加する。ゾーン単位ロックの節に、排他を保持したままの実行とゾーン全体の一括置き換えを加える
- [X] T048 [P] `README.md` 88 行の `dpf/utils` の説明に、排他を保持したままの実行とゾーン全体の一括置き換えを追記する
- [X] T049 [P] `CHANGELOG.md` の `[Unreleased]` に、機能追加（`Mutex.Do`、`ZoneApplier`、Option 3 件、`ErrNoRecords`）と破壊的変更（`dpf.ZonesApi` へのメソッド追加）を記載する（FR-026）
- [X] T050 `internal/integration/lockflow_test.go` の `TestLockFlow_AtomicChanges` を書き換える。期待値を「一括置き換えによって排他が失われる」へ反転し、[research.md](./research.md) D1 の実測を固定する回帰テストにする
- [X] T051 `internal/integration/lockflow_test.go` に `ZoneApplier` の確認を追加する。ゾーンを置き換えられること、SOA と Zone Apex の NS が変わらないこと、**SOA に付けた利用者のラベルが残ること**、置き換えの後に排他も未反映の編集も残らないこと、成功時に `ErrNotLockHolder` が返らないこと（FR-028・SC-003・SC-004・SC-007・SC-009）
- [X] T052 `internal/integration/lockflow_test.go` に `Mutex.Do` の確認を追加する。保持期間を短く設定（1 分、延長の間隔 20 秒）し、保持期間より長い処理でも保持が続くことを実 API で確認する（SC-001、[quickstart.md](./quickstart.md) 第 4 節の判断）
- [X] T053 `internal/integration/lockflow_test.go` に `Mutex.Do` の中で 1 レコードずつ変更して反映し、解放まで通ることの確認を追加する（[contracts/api.md](./contracts/api.md) 第 1 節の約束 5）
- [X] T054 `make check` を実行し、マージ前の 7 ゲートすべてが成功することを確認する。**`make test` が `-race` で走ることが本機能では特に重要である**（[quickstart.md](./quickstart.md) 第 1 節）
- [X] T055 `make test-integration` を実行し、全件が通ることを確認する。**トークン未設定でスキップされた結果を確認結果として報告しない**（憲章 品質ゲート、[quickstart.md](./quickstart.md) 第 4 節）
- [X] T056 [quickstart.md](./quickstart.md) 第 2〜5 節の確認表を上から順に実行し、各項目が満たされていることを確認する

---

## 依存関係と実行順序

### フェーズ間の依存

- **準備 (フェーズ 1)**: 依存なし。ただちに開始できる
- **土台 (フェーズ 2)**: 準備の完了に依存する。すべての利用シナリオを塞ぐ
- **US1 (フェーズ 3)**: 土台の完了に依存する
- **US2 (フェーズ 4)**: 土台と **US1 に依存する**。`Apply` は `doHold` の上に載り、延長を止める通知（T037）は US1 の延長の実装（T019〜T021）が前提である
- **US3 (フェーズ 5)**: 土台の完了に依存する。US1 と独立に実装できる
- **仕上げ (フェーズ 6)**: T043〜T049 は対象の実装の完了に依存する。T050〜T053 はすべての実装の完了に依存する

### 利用シナリオ間の依存

- **US1 (P1)**: 土台の後に開始できる。**単独で価値がある**（排他の下で長い処理を安全に走らせる。1 レコードずつ反映する流れにも使える）ため MVP である
- **US2 (P2)**: US1 に依存する。本機能の発端だが、土台が US1 である
- **US3 (P3)**: US1・US2 と独立。先に実装してもよい

### 各利用シナリオの内部

- テストを先に書き、実装前に失敗することを確認する
- US1 の内部では、延長の goroutine（T019）→ 失敗の分岐（T020）→ 終了待ち（T021）→ エラーの組み立て（T022）の順
- US2 の内部では、型の定義（T032）→ コンストラクタ（T033）→ 部品（T034〜T036）→ 消費の通知（T037）→ 組み立て（T038）の順

### 並行できる箇所

実装の中心が `utils/lock.go`（US1・US3）と `utils/apply.go`（US2）に分かれるため、006 より並行の余地がある。

- T001（`interfaces.go`）は T002〜T005（`utils/lock_test.go`）と並行できる
- US2 の実装（`utils/apply.go` / `utils/apply_test.go`）は、US1 の完了後であれば US3（`utils/lock.go`）と並行できる。ただし T037 は `utils/lock.go` を触るため US3 と競合する
- 仕上げの T043〜T049 は互いに別ファイルであり並行できる（T043 と T045 は同じ `utils/lock.go` を触るため、この 2 つは直列）

複数人で進める場合の分担は「US1 → US2 を 1 人」「US3 を 1 人」「文書（T046〜T049）を 1 人」である。

---

## 並行実行の例: フェーズ 1

```bash
# interfaces.go と模擬サーバは別ファイルなので同時に進められる:
Task: "interfaces.go の ZonesApi を拡張し JobsApi を新設する"
Task: "utils/lock_test.go の模擬サーバに currents / atomic_changes / JOB を追加する"
```

## 並行実行の例: フェーズ 6

```bash
# 文書はそれぞれ別ファイル:
Task: "utils/apply.go の godoc を書く"
Task: "utils/example_test.go の例を差し替える"
Task: "utils/doc.go に 2 つの操作の説明を追加する"
Task: "README.md と CHANGELOG.md を更新する"
```

---

## 実装の進め方

### まず MVP (利用シナリオ 1 のみ)

1. フェーズ 1（準備）を完了する
2. フェーズ 2（土台）を完了する
3. フェーズ 3（US1）を完了する
4. **いったん止めて確認する**: 保持期間より長い処理で保持が続くこと、奪われたら処理が止まることを単体テストで確認する
5. この時点で単独で出せる。1 レコードずつ反映する流れの利用者は、これだけで「取得と解放の対」「延長」を書かなくて済む

### 増分で届ける

1. 準備 + 土台 → 内部構造が揃う
2. US1 → 排他を保持したままの実行（MVP。単独で検証・提供できる）
3. US3 → 待機の選択が加わる（US1 と独立。先でもよい）
4. US2 → ゾーン全体の一括置き換えが加わる。本機能の発端が満たされる
5. 仕上げ → 文書と実 API での確認

### 順序の入れ替え

US3 は US1 と独立であり、先に実装してもよい。ただし US2 は US1 に依存するため、この順序は変えられない。

---

## 補足

- `[P]` のタスク = 異なるファイル、依存なし
- 実装前にテストが失敗することを確認する
- タスクごと、あるいは論理的なまとまりごとにコミットする
- **実装は `main` から新しいブランチを切って始める。** 現在の作業ツリーは `docs/changelog-0.3.0` にあり、007 の設計文書と、D1 の実測に合わせた godoc の訂正が未コミットで残っている（`utils/lock.go`、`utils/doc.go`、`specs/006-zone-lock-redesign/spec.md`）。これらは本機能の作業に含まれるため、同じブランチで扱う
- **`utils/example_test.go` と `internal/integration/lockflow_test.go` は現在、実測と矛盾する状態で作業ツリーに残っている。** 前者は `overwrite_soa=false` なら安全という前提で書いた例（T046 で差し替え）、後者は期待値が反転前のまま（T050 で書き換え）。この 2 つを片付けるまで、作業ツリーは中途半端な状態である
- 本機能は goroutine を使う。`make test` が `-race` で走ることを常に確認する（T054）

---

## 実行結果

**全 56 タスク完了。**

| 確認 | 結果 |
|---|---|
| `make check`（マージ前 7 ゲート） | 通過 |
| `go test ./... -race -count=1` | 全モジュール通過 |
| `golangci-lint run --build-tags=integration ./...` | 0 issues |
| `make test-integration` | **全 22 テスト通過**（約 10 分 44 秒、2026-09-24） |

### 実装中に判明した追加の変更

`dpf.RecordsApi` に **`GetRecordCurrents` の追加が必要**であった。計画（research.md D10）は
`ZonesApi` の拡張と `JobsApi` の新設のみを挙げていたが、公開されているレコードの取得も
部分インターフェース経由で行うため、これも追加した。破壊的変更の種類は同じであり、
`CHANGELOG.md` に記載した。

### テストが効いていることの確認

最も重要な主張（取り込みの可否を決める 2 つのフラグが常に false で送られること）について、
実装を `overwrite_soa=true` へ変異させ、`TestZoneApplier_FlagsAlwaysFalse` が失敗することを
確認した。変異を戻した後は通る。

### 2026-09-24 の統合テストで判明したこと（1 回目、失敗）

**実装ではなくテストの欠陥であった。** 2 件を修正して再実行し、通した。

1. **後始末が「反映済みの生きた排他」を残していた。** `TestLockFlow_ZoneChanges` は
   ゾーン反映によってロックのラベルを反映済みにし、その後の解放が作る未反映の編集を
   `discardSOAChanges` で破棄していた。破棄すると反映済みの側の排他（奪ってよい時刻が
   未来）が復活するため、**ゾーンが保持期間のあいだロックされたままになった。** 後続の
   テストが軒並み `ErrStillLock` で落ちた原因である。後始末を `clearSOALock`（ロックの
   ラベルを取り除いて反映する）へ差し替えた。
2. **保持の判定が owner の一致だけを見ていた。** 過去の実行が残した期限切れのラベルを
   「保持している」と誤判定した。`logSOAState` の判定に有効期限を加えた。

1 件目は利用者にも起こりうる落とし穴であるため、`Mutex` の godoc に明記した
（解放は破棄ではなく反映によって確定させること）。仕様の前提にも追記した。**この性質は
テストが失敗しなければ気づかなかった。**

### 実測で確認できたこと（2 回目、成功）

| 成功基準 | 証拠 |
|---|---|
| SC-001（保持期間より長い処理でも保持が続く） | 奪ってよい時刻が `1790213968` → `1790214035`（+67 秒、保持期間 60 秒）。処理中に別 owner が取得できないことも確認 |
| SC-003（SOA と Zone Apex の NS が置き換わらない） | `TestZoneApplierApply` が通過 |
| SC-004（成功時に解放起因のエラーが返らない） | 同上。`ErrNotLockHolder` は返らなかった |
| SC-007（未反映の編集が残らない） | `[Apply 後] 未反映件数=0` |
| SC-009（利用者のラベルが失われない） | 編集関数へ `applier.test.dpf-go=keep-...` が渡り、Apply の後も残存。SOA の ID は `rjqmfs7z0y8ncw` → `rmythc789x0bqk` と変わっている |

D1b（反映済みのラベルは残る）は、利用者による確認から**実測による確認**になった。
