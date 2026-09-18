---

description: "ゾーン単位の排他の見直しの実装タスク"
---

# タスク: ゾーン単位の排他の見直し

**入力**: `/specs/006-zone-lock-redesign/` の設計文書

**前提**: [plan.md](./plan.md)、[spec.md](./spec.md)、[research.md](./research.md)、[data-model.md](./data-model.md)、[contracts/](./contracts/)、[quickstart.md](./quickstart.md)

**テスト**: **含める。** 憲章 原則 II（NON-NEGOTIABLE）が「手書きコードは単体テストを伴わなければ完成とみなさない」と定める。加えて仕様 FR-028 が実 API での前提確認を、憲章の品質ゲートがロックの変更に対する `make test-integration` の実行を求めている。任意ではなく必須である。

**構成**: タスクは利用シナリオごとにまとめる。ただし本機能は既存の 1 ファイル（`utils/lock.go`）の作り替えであるため、**並行できる箇所は少ない**。`[P]` は異なるファイルを触るタスクにのみ付けている。

## 書式: `[ID] [P?] [シナリオ] 説明`

- **[P]**: 並行して実行できる (異なるファイル、依存なし)
- **[シナリオ]**: このタスクが属する利用シナリオ (US1、US2、US3、US4)
- 説明には正確なファイルパスを含めること

## パスの規約

本機能は既存の Go ライブラリへの変更である。新規ファイルは作らない。

- 本体パッケージ: リポジトリルートの `interfaces.go`
- 実装とその単体テスト: `utils/lock.go`、`utils/lock_test.go`
- 文書: `utils/doc.go`、`utils/example_test.go`、`CHANGELOG.md`
- 実 API の確認: `internal/integration/`

---

## フェーズ 1: 準備 (共通の基盤)

**目的**: 実装方針の裏取りと、テストの足場づくり

- [X] T001 `make test-integration` を実行し、`internal/integration/mutex_test.go` が出力する「ロック前の SOA」「ロック後の SOA」のログから、`GetRecordList` が返す SOA の行数と state を確認する。2 行以上返る場合（state=5 の行が現れる場合）は [research.md](./research.md) D10 の判定が必須であることの裏取りとなる。結果を T010 の実装判断に用いる
- [X] T002 [P] `interfaces.go` の `RecordsApi` に `PostRecord` / `DeleteRecord` / `DeleteRecordChanges` を追加し、`var _ RecordsApi = (*RecordsAPIService)(nil)` が通ることを確認する（[research.md](./research.md) D5）
- [X] T003 `utils/lock_test.go` の模擬サーバ `lockServer` を拡張し、`POST /zones/{id}/records`、`DELETE /zones/{id}/records/{rid}`、`DELETE /zones/{id}/records/{rid}/changes` を扱えるようにする。レコードを ID 付きで保持し、state（0/1/2/3）を表現できる形にする
- [X] T004 `utils/lock_test.go` の模擬サーバに、同名同 RRTYPE の追加に対して `error_details` に `code=duplicated, attribute=record` を含む 400 を返す挙動を実装する。削除予定のレコードは重複判定に含めない（[research.md](./research.md) D1 の実測どおり）

**関門**: 模擬サーバが専用レコードのライフサイクル（追加予定・反映済み・削除予定・取り消し）を表現でき、重複拒否を再現できる

---

## フェーズ 2: 土台 (先行必須)

**目的**: すべての利用シナリオが依存する部品。`Mutex` の内部構造、設定、共通判定

**⚠️ 重要**: このフェーズが完了するまで、利用シナリオの作業は開始できない

- [X] T005 `utils/lock.go` に新しい定数を追加する。`DefaultLockRecordTTL`（1 分）、`DefaultVerifyTimeout`（10 秒）、`DefaultLockRecordLabel`（`_dpf-go-lock`）。既存の `DefaultLockTTL`（15 分）と `DefaultLockOwner` は変更しない（[data-model.md](./data-model.md) 第 4 節）
- [X] T006 `utils/lock.go` に番兵エラー `ErrNotLockHolder` と `ErrLabelLimit` を追加し、godoc を付ける（[contracts/mutex.md](./contracts/mutex.md) 第 4 節）
- [X] T007 `utils/lock.go` の `Mutex` 構造体に `lockRecordTTL` / `verifyTimeout` / `lockRecordLabel` / `lockRecordRrtype` / `lockRecordRdata` / `zoneName` を追加する。埋め込み `sync.Mutex` は同一インスタンスへの呼び出しの直列化として維持する
- [X] T008 `utils/lock.go` に Option を 4 件追加する。`WithLockRecordTTL` / `WithVerifyTimeout` / `WithLockRecordLabel` / `WithLockRecordContent`。0 値・空値は無視して既定のままとする既存の作法に合わせる（[contracts/mutex.md](./contracts/mutex.md) 第 3 節）
- [X] T009 `utils/lock.go` の `defaultOwner` を書き換え、`<ホスト名の nodename>-<PID>-<ランダム 4 バイトの 16 進>` を `NewMutex` の呼び出しごとに生成する。`crypto/rand` を用い、全体が 63 文字に収まるようホスト名側を切り詰める（[data-model.md](./data-model.md) 第 3 節、[research.md](./research.md) D9）
- [X] T010 `utils/lock.go` の `getSOA` を書き換える。state=3 の行を優先し、無ければ state=0 の行を使う。どちらも無い場合、または同じ state が複数ある場合はエラーを返す（[research.md](./research.md) D10。T001 の結果を反映する）
- [X] T011 `utils/lock.go` に、SOA の `name` からゾーン名を導き `Mutex` に保持する処理を追加する。2 回目以降は問い合わせを省く（[research.md](./research.md) D4）
- [X] T012 `utils/lock.go` に、専用レコードの FQDN を「最左ラベル + ゾーン名」で組み立てる処理を追加する。結合と正規化は `github.com/miekg/dns` の `dns.Fqdn` / `dns.CanonicalName` で行う（憲章 原則 IV、[research.md](./research.md) D3）
- [X] T013 `utils/lock.go` に、ラベル数の上限判定を追加する。書き込み後のラベル数が 10 を超える場合は `ErrLabelLimit` を返す。既にロックのラベルが付いている場合は増えない（FR-014、[data-model.md](./data-model.md) 第 1 節）
- [X] T014 `utils/lock.go` に `verify`（反映確認）を実装する。`GetRecordList` を `verifyTimeout` まで繰り返し、自分が書いた owner と奪ってよい時刻がそのまま読み出せることを確認する。読み出せない場合は `ErrStillLock` を返す。`ctx` の打ち切りに従う（FR-020、[research.md](./research.md) D6）
- [X] T015 `utils/lock.go` に、API エラーから重複拒否を判別する処理を追加する。`*dpf.GenericOpenAPIError` から `ParameterErrorResponse` を取り出し、`error_details` に `code=duplicated, attribute=record` が含まれるかを見る（[contracts/lock-record.md](./contracts/lock-record.md) 第 3 節）
- [X] T016 `utils/lock.go` の `lockable` を書き換える。判定は奪ってよい時刻のみで行い、owner 一致の分岐を削除する。数値として解釈できない値は保持者不明として取得を許す（FR-018・FR-019、[research.md](./research.md) D8）
- [X] T017 `utils/lock_test.go` に土台のテストを追加する。既定 owner がインスタンスごとに異なり 63 文字以内でホスト名と PID を含むこと、`lockable` が owner を見ないこと、不正な deadline で取得を許すこと、`getSOA` が state=3 を優先すること、ラベル上限で `ErrLabelLimit` になること

**関門**: 土台の単体テストが通り、`go build ./...` と `make check-lint` が成功する。この時点で `Lock` はまだ旧来の動作でよい

---

## フェーズ 3: 利用シナリオ 1 - 同一ユーザの複数プログラムでも二重に取得されない (優先度: P1) 🎯 MVP

**目標**: 一時レコード追加ロックの保護下で SOA ラベルの排他を取得する。事前判定で取得できないと分かる場合は専用レコードを作らない。取得は再入できない

**独立した検証**: 同じゾーンに対して `Mutex` を 2 つ作り、一方が取得している間はもう一方が取得できないことを確認する。同一プロセス内でも同じであることを確認する。取得の前後で専用レコードが残らないことを確認する

### 利用シナリオ 1 のテスト ⚠️

> **注記: これらのテストを先に書き、実装前に失敗することを確認する**

- [X] T018 [US1] `utils/lock_test.go` に取得の基本のテストを追加する。排他が無い状態で取得でき、SOA に owner と奪ってよい時刻が書かれ、取得後に専用レコードが残らないこと（FR-001・FR-009）
- [X] T019 [US1] `utils/lock_test.go` に再入禁止のテストを追加する。自分自身が保持している状態での取得が `ErrStillLock` になること（FR-017）
- [X] T020 [US1] `utils/lock_test.go` に事前判定のテストを追加する。他者が保持中のとき、専用レコードの追加 API を呼ばずに `ErrStillLock` を返すこと。ラベル上限の場合も追加 API を呼ばないこと（FR-002・SC-008）
- [X] T021 [US1] `utils/lock_test.go` に判定の食い違いのテストを追加する。事前判定では取得できたが一時ロック取得後の判定では取得できない場合、ラベルを書き込まず一時ロックを解放して `ErrStillLock` を返すこと（FR-003）
- [X] T022 [US1] `utils/lock_test.go` に一時ロックの競合のテストを追加する。専用レコードが追加予定で失効前なら `ErrStillLock` になること（FR-007）。追加予定が 2 件あるとき ID の昇順で勝者が決まり、敗者は自分の追加予定を取り消して `ErrStillLock` を返すこと（[research.md](./research.md) D7）
- [X] T023 [US1] `utils/lock_test.go` に既存ラベルの保持のテストを追加する。SOA に付いている他のラベルが変更・削除されないこと（FR-013）

### 利用シナリオ 1 の実装

- [X] T024 [US1] `utils/lock.go` に一時レコード追加ロックの取得を実装する。専用レコードを追加予定で作成し、ラベルに owner と失効時刻を入れる。重複拒否は T015 で判別する（FR-004・FR-005）
- [X] T025 [US1] `utils/lock.go` に一時レコード追加ロックの解放を実装する。`DeleteRecordChanges` で追加予定を取り消し、消えたことを確認する。解放に失敗した場合は取得を失敗として返す（FR-009、[contracts/lock-record.md](./contracts/lock-record.md) 第 4 節）
- [X] T026 [US1] `utils/lock.go` に追加予定が 2 件並んだ場合の勝者判定を実装する。ID の昇順で最小の 1 件を勝者とし、自分が勝者でなければ解放して `ErrStillLock` を返す（[research.md](./research.md) D7）
- [X] T027 [US1] `utils/lock.go` の `Lock` を [contracts/lock-record.md](./contracts/lock-record.md) 第 2 節の 10 段の順序に書き換える。事前判定 → 一時ロック取得 → 保護下での再読み取りと本判定 → 書き込み → 反映確認 → 一時ロック解放。取得の成否によらず一時ロックを解放する（FR-001〜FR-003・FR-009）
- [X] T028 [US1] `utils/lock.go` の `LockWait` が新しい `Lock` の上で動くことを確認し、`ErrStillLock` 以外のエラーで打ち切る既存の挙動を維持する（FR-027）

**関門**: 利用シナリオ 1 の単体テストが通る。同一ユーザ・同一プロセスでの二重取得が起きないことが単体テストで確認できている（SC-001・SC-002 の単体レベル）

---

## フェーズ 4: 利用シナリオ 2 - 中断しても必ず回復する (優先度: P2)

**目標**: 専用レコードが追加予定のまま残った場合は失効後に取り消して取得できる。公開されてしまった場合は削除予定にして入れ直す

**独立した検証**: 専用レコードを追加予定のまま残した状態、および反映済みの状態を作り、それぞれから取得が成功することを確認する

### 利用シナリオ 2 のテスト ⚠️

- [X] T029 [US2] `utils/lock_test.go` に失効した一時ロックのテストを追加する。専用レコードが追加予定で失効時刻を過ぎている場合、取り消してから追加をやり直して取得できること。やり直しは 1 回までであること（FR-006）
- [X] T030 [US2] `utils/lock_test.go` に公開済みからの回復のテストを追加する。専用レコードが反映済みの場合、削除予定にしてから追加予定を入れて取得でき、**削除予定が残る**こと。ゾーン反映の API を呼ばないこと（FR-008）
- [X] T031 [US2] `utils/lock_test.go` に回復時の競合のテストを追加する。取り消しや削除が「対象が存在しない」「既に削除予定である」として拒否された場合、失敗とせずに追加へ進むこと（FR-008）
- [X] T032 [US2] `utils/lock_test.go` に不正な失効時刻のテストを追加する。専用レコードの失効時刻が数値として解釈できない場合、保持者不明として取り消して取得できること

### 利用シナリオ 2 の実装

- [X] T033 [US2] `utils/lock.go` に失効判定と取り消しのやり直しを実装する。既存の専用レコードの失効時刻を読み、過ぎている場合は `DeleteRecordChanges` してから `PostRecord` をやり直す。やり直しは 1 回の取得につき 1 度まで（FR-006・FR-007）
- [X] T034 [US2] `utils/lock.go` に公開済み専用レコードからの回復を実装する。state=0 の専用レコードは `DeleteRecord` で削除予定にしてから `PostRecord` をやり直す。**削除予定は取り消さない。** ゾーン反映は行わない（FR-008）
- [X] T035 [US2] `utils/lock.go` の回復経路で、取り消し・削除の拒否を失敗として扱わず追加へ進む処理を実装する（FR-008、[contracts/lock-record.md](./contracts/lock-record.md) 第 3 節）

**関門**: 利用シナリオ 2 の単体テストが通る。中断のどの状態からも手作業なしに回復できることが確認できている（SC-006）

---

## フェーズ 5: 利用シナリオ 3 - 保持期間の延長を取得と分けて行える (優先度: P3)

**目標**: 延長を `Renew` として独立させ、延長と解放に保持者の確認を伴わせる

**独立した検証**: 排他を取得した状態で延長し、奪ってよい時刻が進むことを確認する。保持者でない状態での延長・解放が判別できるエラーになり、他者の排他が変更されないことを確認する

### 利用シナリオ 3 のテスト ⚠️

- [X] T036 [US3] `utils/lock_test.go` に延長のテストを追加する。保持している状態で `Renew` を呼ぶと奪ってよい時刻が TTL の分だけ進むこと。一時ロックの API を呼ばないこと（FR-022、[research.md](./research.md) D11）
- [X] T037 [US3] `utils/lock_test.go` に保持者でない場合のテストを追加する。保持していない状態、および保持中に他者へ奪われた状態での `Renew` が `ErrNotLockHolder` になること（FR-023）
- [X] T038 [US3] `utils/lock_test.go` に解放のテストを追加する。保持者であれば奪ってよい時刻が現在時刻になること、他者が保持している場合は `ErrNotLockHolder` を返し**他者の排他を変更しない**こと、排他が無い場合は何もせず成功すること（FR-024・FR-025）

### 利用シナリオ 3 の実装

- [X] T039 [US3] `utils/lock.go` に `Renew` を実装する。SOA の owner が自分であることを確認したうえで奪ってよい時刻を進め、`verify` で確認する。確認できない場合は `ErrNotLockHolder` を返す。一時ロックは取得しない（FR-022・FR-023、[contracts/lock-record.md](./contracts/lock-record.md) 第 5 節）
- [X] T040 [US3] `utils/lock.go` の `Unlock` に保持者の確認を追加する。owner が自分でない場合は他者の排他を変更せず `ErrNotLockHolder` を返す。deadline ラベルが無い場合は何もせず成功する（FR-024・FR-025）
- [X] T041 [P] [US3] `utils/example_test.go` に `Renew` の使用例を追加し、`Lock` が再入できないことが例から読み取れるようにする

**関門**: 利用シナリオ 3 の単体テストが通る。延長と解放が保持者に対してのみ働くことが確認できている

---

## フェーズ 6: 利用シナリオ 4 - 排他の操作がゾーンへ反映されない (優先度: P4)

**目標**: 手順が 4 段になり専用レコードを扱うようになっても、ゾーン反映が発生しないことを保証する（回帰防止）

**独立した検証**: 取得・延長・解放のそれぞれについて、ゾーン反映の API が呼ばれないことと、権威サーバに公開されるレコードが変化しないことを確認する

### 利用シナリオ 4 のテスト ⚠️

- [X] T042 [US4] `utils/lock_test.go` に、取得・延長・解放のいずれでも `PATCH /zones/{id}/changes` と `PATCH /zones/{id}/atomic_changes` が呼ばれないことのテストを追加する。模擬サーバがこれらを受けたらテストを失敗させる（FR-026・SC-004）
- [X] T043 [US4] `internal/integration/mutex_test.go` を更新する。「同じ owner なら再取得できる」の期待値を `ErrStillLock` へ反転し（117-120 行）、`Renew` で奪ってよい時刻が進むことの確認を追加する（FR-017・FR-022）
- [X] T044 [US4] `internal/integration/mutex_test.go` に、取得の前後で専用レコード（`_dpf-go-lock` の TXT）が 1 件も存在しないこと、および未反映件数が SOA の 1 件だけであることの確認を追加する（SC-005、FR-009）

### 利用シナリオ 4 の実装

- [X] T045 [US4] `internal/integration/README.md` の「実行が中断された場合」（102-111 行）を書き換える。owner 固定による自己回復は成立しなくなるため、後始末（`discardSOAChanges`）、ロック TTL の経過、一時ロックの失効の 3 つに基づく説明にする（[research.md](./research.md) D13）

**関門**: すべての利用シナリオが単体テストで確認でき、`make test` が通る

---

## フェーズ 7: 仕上げと横断的な事項

**目的**: 文書、実 API での確認、同時性の確認

- [X] T046 [P] `utils/lock.go` の `Mutex` の godoc を書き換える。[contracts/mutex.md](./contracts/mutex.md) 第 5 節の 6 点（競合相手ごとの排他の強さ、再入禁止と `Renew`、`WithOwner` の一意性が壊すもの、ゾーン反映なし、専用レコードの公開とその回復、ラベル 2 つの消費）を含める（FR-029）
- [X] T047 [P] `utils/doc.go` の 16 行・84-87 行の排他の説明を書き換え、競合相手ごとの強さの違いに触れる（FR-029）
- [X] T048 [P] `CHANGELOG.md` の `[Unreleased]` に、破壊的変更 5 件（再入禁止、既定 owner の形式、延長・解放の保持者確認、`RecordsApi` の 3 メソッド追加、取得判定の変更）と修正（同一ユーザ間で排他が成立していなかった点、`getSOA` の行の選び方）を記載する（FR-030、[plan.md](./plan.md)「破壊的変更の一覧」）
- [X] T049 [P] `specs/003-utils-highlevel-api/spec.md` に、利用シナリオ 4 と FR-039〜FR-045・SC-013〜SC-015 が本仕様（006）に置き換えられた旨を追記し、双方から辿れるようにする（FR-031）
- [X] T050 `make check` を実行し、マージ前の 7 ゲートすべてが成功することを確認する（[quickstart.md](./quickstart.md) 第 1 節）
- [X] T051 `make test-integration` を実行し、`TestZoneMutex` と `TestLockRecordPremises` を含む全件が通ることを確認する。**トークン未設定でスキップされた結果を確認結果として報告しない**（憲章 品質ゲート、[quickstart.md](./quickstart.md) 第 4 節）
- [X] T052 同一トークンで 2 つのプロセスを同時に起動し、成立する排他が 1 つだけであることを手動で確認する。`internal/integration` は API 呼び出しを直列化するため自動化しない（SC-001、[quickstart.md](./quickstart.md) 第 4 節の判断）
- [X] T053 [quickstart.md](./quickstart.md) 第 2〜5 節の確認表を上から順に実行し、各項目が満たされていることを確認する

---

## 依存関係と実行順序

### フェーズ間の依存

- **準備 (フェーズ 1)**: 依存なし。ただちに開始できる。T001 は実 API を要する
- **土台 (フェーズ 2)**: 準備の完了に依存する。すべての利用シナリオを塞ぐ
- **利用シナリオ 1 (フェーズ 3)**: 土台の完了に依存する
- **利用シナリオ 2 (フェーズ 4)**: 土台と**利用シナリオ 1 に依存する**。回復経路は一時レコード追加ロックの取得処理（T024）の分岐として実装されるため、US1 の後に置く
- **利用シナリオ 3 (フェーズ 5)**: 土台の完了に依存する。US1 とは独立に実装できる（`Renew` / `Unlock` は一時ロックを使わない）
- **利用シナリオ 4 (フェーズ 6)**: US1〜US3 の実装が揃っていることに依存する（回帰の確認であるため）
- **仕上げ (フェーズ 7)**: すべての利用シナリオの完了に依存する

### 利用シナリオ間の依存

本機能は既存の 1 つの型（`utils.Mutex`）の作り替えであり、テンプレートが想定する「シナリオごとに独立した構成要素」には当てはまらない。実際の依存は次のとおり。

- **US1 (P1)**: 土台の後に開始できる。**これ単独で仕様の中心（同一ユーザ間の排他）を満たすため MVP である**
- **US2 (P2)**: US1 の一時ロック取得処理に分岐を足す形になるため、US1 に依存する
- **US3 (P3)**: US1 と独立。`Renew` / `Unlock` は SOA のラベルのみを扱う。**US1 より先に実装することもできる**
- **US4 (P4)**: 回帰防止であり、US1〜US3 の後

### 各利用シナリオの内部

- テストを先に書き、実装前に失敗することを確認する
- 土台（フェーズ 2）の部品は、利用シナリオの実装より先
- `Lock` の組み立て（T027）は、一時ロックの取得・解放・勝者判定（T024〜T026）の後

### 並行できる箇所

**少ない。** 実装の大半が `utils/lock.go`、単体テストの大半が `utils/lock_test.go` に集まるため、同一ファイルの競合を避けると直列になる。`[P]` を付けたのは次のみ。

- T002（`interfaces.go`）は T003・T004（`utils/lock_test.go`）と並行できる
- T041（`utils/example_test.go`）は US3 の他のタスクと並行できる
- T046〜T049（`utils/lock.go` の godoc・`utils/doc.go`・`CHANGELOG.md`・`specs/003-.../spec.md`）は互いに並行できる。ただし T046 は `utils/lock.go` を触るため、フェーズ 6 までの実装が終わってから行う

複数人で進める場合の現実的な分担は「US1 + US2 を 1 人」「US3 を 1 人」「文書（T046〜T049）を 1 人」である。`utils/lock.go` を 2 人で同時に触ることは避ける。

---

## 並行実行の例: フェーズ 1

```bash
# interfaces.go とテストの足場は別ファイルなので同時に進められる:
Task: "interfaces.go の RecordsApi に 3 メソッドを追加する"
Task: "utils/lock_test.go の模擬サーバを POST / DELETE / changes へ拡張する"
```

## 並行実行の例: フェーズ 7

```bash
# 文書はそれぞれ別ファイル:
Task: "utils/doc.go の排他の説明を書き換える"
Task: "CHANGELOG.md の [Unreleased] に破壊的変更を記載する"
Task: "specs/003-utils-highlevel-api/spec.md に置き換えの旨を追記する"
```

---

## 実装の進め方

### まず MVP (利用シナリオ 1 のみ)

1. フェーズ 1（準備）を完了する。**T001 の実測結果が T010 の実装に影響する**
2. フェーズ 2（土台）を完了する
3. フェーズ 3（利用シナリオ 1）を完了する
4. **いったん止めて確認する**: 同一ユーザ・同一プロセスでの二重取得が起きないことを単体テストで確認する
5. この時点で仕様の中心は満たされている。ただし**公開してはならない**。US3 の `Renew` が無いと、再入禁止（T019）によって延長の手段が失われ、利用者が保持期間を延ばせなくなる

### 増分で届ける

1. 準備 + 土台 → 部品が揃う
2. US1 → 同一ユーザ間の排他が成立する（MVP。単独で検証可能）
3. US3 → 延長の手段が戻る。**US1 と US3 が揃った時点が、利用者へ出せる最小の組み合わせ**
4. US2 → 中断からの回復が揃う
5. US4 → 回帰防止の確認が揃う
6. 仕上げ → 文書と実 API での確認

### 順序の入れ替え

US3（`Renew`）は US1 と独立であり、先に実装してもよい。US1 が再入禁止を導入する前に延長の手段を用意しておくと、途中の状態でも利用者から見た機能が欠けない。**この順序を推奨する。**

---

## 補足

- `[P]` のタスク = 異なるファイル、依存なし
- `[シナリオ]` のラベルは、追跡のためにタスクを特定の利用シナリオへ対応づける
- 実装前にテストが失敗することを確認する
- タスクごと、あるいは論理的なまとまりごとにコミットする
- 各フェーズの関門で作業を止めて確認できる
- 本機能は破壊的変更を含む。`CHANGELOG.md` への記載（T048）は最後ではなく、破壊的変更を入れた時点で書き足してもよい
- T052（同時性の手動確認）は自動化しない。理由は [quickstart.md](./quickstart.md) 第 4 節に記録している

---

## 実行結果 (2026-09-18)

**全 53 タスク完了。**

| 確認 | 結果 |
|---|---|
| `make check`（マージ前 7 ゲート） | 通過 |
| `make test-integration` | 全 18 テスト通過（約 3 分 57 秒） |
| 単体テスト | 28 件通過（`go test ./utils/ -race`） |
| 同時取得（T052、SC-001） | 2 プロセス同時起動で取得成功は 1 つだけ |

### T001 の実測結果

`GetRecordList` が返す SOA の行は **1 行のみ**であった（ロック前 state=0、ロック後 state=3、
いずれも同じ ID `rzxbovrgki2lnd`）。state=5（更新前の状態）の行は現れない。

したがって [research.md](./research.md) D10 が想定した「反映済みの古い行を掴んで保持中の
排他を他者へ渡す」事象は、現行の DPF-API では発生していなかった。state=3 を優先する実装は
**既存の欠陥の修正ではなく、応答が変わった場合に備えた予防**である。この区別を
`CHANGELOG.md`、research.md D10、`mutex_test.go` のコメントへ反映した。

### T052 の実測結果

開始時刻を揃えた 2 プロセスを同一のアクセストークンで同時に起動し、取得に成功したのは
1 つだけであった。

```text
RESULT acquired=true  owner=...-1601400-8f94d60a elapsed=6.941s
RESULT acquired=false owner=...-1601399-3dca85f8 elapsed=16.312s err=dpf: zone is still locked
```

敗者の 16.3 秒は、自分の追加予定が一覧へ現れるのを待って反映確認の上限（既定 10 秒）に
達した経路と整合する（[research.md](./research.md) D7 の待機）。この経路では自分の専用
レコードが追加予定のまま残りうるが、失効時刻の経過後に次の取得が取り消す。

確認に用いたコマンドは `internal/integration/manual` に置いた。手順は
[quickstart.md](./quickstart.md) 第 4 節と `internal/integration/README.md` を参照。
