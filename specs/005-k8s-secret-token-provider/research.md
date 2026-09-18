# フェーズ 0: 調査

**機能**: Kubernetes Secret からのトークン取得 | **日付**: 2026-09-18

**仕様**: [spec.md](./spec.md) | **計画**: [plan.md](./plan.md)

仕様に未解決の要明確化は無い。本フェーズで決めたのは、計画に委ねられていた技術的な選択である。
実測はすべて go1.27.1 linux/amd64、`k8s.io/client-go v0.37.0` で行った。

---

## D1: Kubernetes SDK の選定

**決定**: `k8s.io/client-go` v0.37.0 を用いる。`k8s.io/api` と `k8s.io/apimachinery` が
連れてくる依存として入る。

**根拠**: Kubernetes の公式クライアントであり、API サーバとの通信、接続設定の読み込み
(`tools/clientcmd`)、クラスタ内の資格情報の読み込み (`rest.InClusterConfig`)、
エラーの分類 (`apimachinery/pkg/api/errors`) のすべてを持つ。本機能が必要とする 4 つが
1 つの依存で揃う。既存の 4 モジュールがいずれも各サービスの公式 SDK を用いている
(spec の前提) のに揃う。

**検討した代替案**:

| 案 | 却下の理由 |
|---|---|
| API サーバへ自前で HTTP リクエストを送る | 依存は最小になるが、kubeconfig の形式 (merge 規則・exec credential plugin・client 証明書) と TLS の組み立てを自前で持つことになる。kubeconfig は利用者の環境そのものであり、自前実装の非互換は利用者の既存の設定が動かないという形で現れる |
| `k8s.io/client-go` のうち `rest` だけを使い typed client を避ける | Secret の取得は 1 エンドポイントなので可能だが、`clientcmd` を使う時点で依存の大半は入る。typed client を避けても削れる依存が無い |

---

## D2: 依存ライセンスと脆弱性の実測

**決定**: `misc/k8s` の依存は憲章の許容リストに収まる。**ソース提供義務を伴う依存
(MPL-2.0) は 1 件も入らない。**

**実測** (2026-09-18。`k8s.io/client-go`、`k8s.io/apimachinery`、`tools/clientcmd`、
`rest`、`kubernetes` を import する probe モジュールに対し `go-licenses csv ./...`。
母集団は憲章が定める「実際に import されるパッケージ」):

| ライセンス | 件数 | 区分 |
|---|---|---|
| Apache-2.0 | 30 | notice |
| BSD-3-Clause | 17 | notice |
| MIT | 6 | notice |
| ISC | 1 (`github.com/davecgh/go-spew/spew`) | notice |
| **合計** | **54** | すべて notice |

- MPL-2.0 / GPL / AGPL / LGPL / SSPL: **0 件**
- 判別不能: 0 件 (probe モジュール自身の 1 件は `LICENSE` を置かない使い捨ての
  スクラッチであり、実モジュールでは FR-004 により `LICENSE` を同梱する)

**実装後の実測 (2026-09-18、`misc/k8s` に対して)**: Apache-2.0 31 / BSD-3-Clause 17 /
MIT 6 / ISC 1 の計 **55 件**。判別不能 0 件、MPL-2.0 と GPL 系は 0 件。
`make check-licenses` は終了コード 0 であった。probe の 54 件との差は 1 件で、
`misc/k8s` 自身が `LICENSE` (Apache-2.0) を持つため母集団に Apache-2.0 として
数えられたものである。以後の比較はこの 55 件を基準とする。
- `govulncheck ./...`: **到達可能な既知脆弱性なし** (`No vulnerabilities found.`)

**この結果の意味**: `misc/vault` は MPL-2.0 を 10 件持ち込んでおり、憲章はこれを
「`misc/vault` の独立モジュールに閉じている」として許容している。`misc/k8s` は
reciprocal な依存を 1 件も持ち込まないため、この観点での追加の負担が無い。
依存の**数**は 54 件で最も重い部類だが、すべて notice 区分である。

**注意**: 本節の実測は probe モジュールに対するものである。実装後に
`make check-licenses` と `make check-vuln` を `misc/k8s` に対して実行し、
値が一致することを確認する (quickstart.md の検証手順に含める)。

---

## D3: 接続情報の選択は client-go に委ねる

**決定**: FR-020 の候補の選択と接続設定の組み立てを `k8s.io/client-go` に委ねる。
`clientcmd.NewNonInteractiveDeferredLoadingClientConfig(clientcmd.NewDefaultClientConfigLoadingRules(), &clientcmd.ConfigOverrides{})`
を作り、その `ClientConfig()` の結果を用いる。独自の順序は持たない。

**根拠**: 利用者が普段使っているコマンドと同じ接続先を向くことが、驚きの少ない挙動で
ある。順序を独自に定めると、同じ環境で `kubectl` と本ライブラリが別のクラスタを指しうる。
順序を仕様と揃えるのではなく、**仕様を標準の順序に合わせる**方針を利用者との確認を経て
決めた (2026-09-18)。

**client-go の順序** (実測したソースによる):

1. `NewDefaultClientConfigLoadingRules()` — 環境変数 `KUBECONFIG` が設定されていれば
   その値 (`filepath.SplitList` で複数パスに分割し、`clientcmd` の merge 規則で統合)、
   設定されていなければ `~/.kube/config` を読む。**両者は同じ「設定ファイル」という
   1 つの候補であり、片方が使われればもう片方は読まれない。**
2. `DeferredLoadingClientConfig.ClientConfig()` — 設定ファイルから接続情報が得られれば
   それを返す。得られない場合に、クラスタ内の資格情報が利用可能であればそれを使う。
3. どちらも駄目なら `ErrEmptyConfig` を返す。

```go
// merged_client_builder.go の要点
mergedConfig, err := mergedClientConfig.ClientConfig()
switch {
case err != nil:
	if !IsEmptyConfig(err) {
		return nil, err          // 空でないエラーは即座に返す。次の候補へ移らない
	}
case mergedConfig != nil:
	if !config.loader.IsDefaultConfig(mergedConfig) {
		return mergedConfig, nil // 設定ファイルが使えたので、それを使う
	}
}
if config.icc.Possible() {           // ここで初めてクラスタ内の資格情報
	return config.icc.ClientConfig()
}
return mergedConfig, err             // どちらも駄目 → ErrEmptyConfig
```

**FR-022 と FR-023 の区別が client-go の挙動に対応する**。
`ClientConfigLoadingRules.Load()` を実測した結果は次のとおりで、仕様の 2 条文は
この挙動をそのまま写したものである。

| 設定ファイルの状態 | client-go の扱い | 対応する要件 |
|---|---|---|
| 存在しない | 読み飛ばす (`os.IsNotExist` なら `missingList` へ入れて継続)。結果として候補が無いものとして扱われ、クラスタ内の資格情報へ進む | FR-023 |
| 存在するが解釈できない | `errlist` へ入り `Load()` がエラーを返す。クラスタ内の資格情報へ移らない | FR-022 |
| 存在し、解釈できるが接続情報として空 | `IsEmptyConfig` に該当し、クラスタ内の資格情報へ進む | FR-023 |

**エラーの対応づけ**: 候補が 1 つも無い場合、`ClientConfig()` は `ErrEmptyConfig` を
返す。`clientcmd.IsEmptyConfig(err)` で判別できるため、これを `ErrNoCredentials`
(FR-021) へ対応づける。それ以外のエラーは包んで返す (FR-022)。

**実装時に確認した追加の分岐** (2026-09-18): `IsDefaultConfig` は
`ClientConfigLoadingRules.DefaultClientConfig` が nil のとき常に false を返す。
`NewDefaultClientConfigLoadingRules()` はこれを設定しないため、本機能の経路では
「設定ファイルから接続情報が得られた」＝「設定ファイルが選ばれた」となり、
分岐は単純になる。また `current-context` が解決できない設定は
`IsEmptyConfig` に該当しないエラーとなり (FR-022)、`current-context` が
書かれていない設定は `IsEmptyConfig` に該当して次の候補へ進む (FR-023)。

**初稿からの変更**: 当初は利用者の記述に列挙された順 (`KUBECONFIG` → クラスタ内の
資格情報 → `~/.kube/config`) を独自に実装する計画だった。`KUBECONFIG` と
`~/.kube/config` の間にクラスタ内の資格情報を挟む形は標準的なクライアントに存在せず、
列挙は「この 3 つを見る」ことを示したものと解した。本決定により、自前で書く選択の
ロジックは無くなる。

**検討した代替案**:

| 案 | 却下の理由 |
|---|---|
| 独自に 3 候補の順序を実装する (初稿の案) | 同じ環境で利用者のコマンドと別の接続先を向きうる。利用者の確認により却下 |
| `ClientConfigLoadingRules.Precedence` に 3 つを並べる | `Precedence` は kubeconfig 形式のファイルの一覧であり、クラスタ内の資格情報 (bearer token の生ファイル) は形式が異なるため並べられない |

---

## D4: 名前空間の決定を client-go の `Namespace()` に委ねられない

**決定**: 名前空間は自前で決定する。`clientcmd` の `Namespace()` は使わない。
kubeconfig の場合は `RawConfig()` から `Contexts[CurrentContext].Namespace` を直接読み、
空であれば FR-014 のエラーとする。クラスタ内の資格情報の場合は
`/var/run/secrets/kubernetes.io/serviceaccount/namespace` を直接読み、
読めない・空であればエラーとする。

**根拠**: **client-go の名前空間取得は、決まらない場合に黙って `"default"` を返す。**
FR-014 が禁じている推測そのものであり、しかも `overridden` フラグは `false` のままなので
呼び出し側から「明示されたのか補われたのか」を区別できない。実測したソースは次のとおり。

```go
// DirectClientConfig.Namespace() — kubeconfig の場合
if len(configContext.Namespace) == 0 {
	return "default", false, nil
}

// inClusterClientConfig.Namespace() — クラスタ内の場合
if ns := os.Getenv("POD_NAMESPACE"); ns != "" { return ns, false, nil }
if data, err := os.ReadFile(".../serviceaccount/namespace"); err == nil {
	if ns := strings.TrimSpace(string(data)); len(ns) > 0 { return ns, false, nil }
}
return "default", false, nil
```

`RawConfig()` は解釈前の設定を返すため、`Namespace` が空であることをそのまま観測できる。
これが FR-014 を満たせる唯一の経路である。

**この決定が spec を裏づける**: 利用者の指摘により FR-014 を「エラー」と定めた
(spec の前提, 2026-09-18) が、その判断はここで技術的にも裏づけられた。仮に `"default"` を
採る設計にしても、client-go 由来の `"default"` と利用者が明示した `"default"` を
区別できないため、どちらが使われたのかを利用者へ説明できない。

**接続情報の選択 (D3) は合わせるのに、名前空間は合わせない理由**: client-go の
`DeferredLoadingClientConfig.Namespace()` は、設定ファイルの名前空間が空のときに
**クラスタ内の名前空間へ回り込み**、それも無ければ `"default"` を補う。つまり
「設定ファイルの資格情報で、クラスタ内の名前空間を読む」組み合わせが起こりうる。

本機能はこれを採らない (FR-013、受け入れシナリオ 2-12)。接続先を取り違えた場合は
認証が失敗して利用者が気づけるが、名前空間を取り違えた場合は**別の Secret を読んで
成功しうる**。誤りの重さが違うため、接続先の選択だけを標準に合わせ、名前空間は
選ばれた認証情報に紐づける。

**選ばれた候補を知る必要がある**: 上記の結果、名前空間の導出元を決めるために
「設定ファイルとクラスタ内の資格情報のどちらが選ばれたか」を知る必要がある。
`ClientConfig()` はこれを返さないため、D5 の `Possible()` と同じ条件、および
設定ファイルから接続情報が得られたかどうかを本パッケージ側でも判定する。
判定条件を client-go と一致させることで、選択そのものは client-go に委ねたまま
(D3)、導出元の判断だけを取り出す。

---

## D5: 「クラスタ内の資格情報が利用可能」の判定

**決定**: client-go の `inClusterClientConfig.Possible()` の判定をそのまま用いる。
独自の条件を定めない。

**実測した条件**:

```go
func (config *inClusterClientConfig) Possible() bool {
	fi, err := os.Stat("/var/run/secrets/kubernetes.io/serviceaccount/token")
	return os.Getenv("KUBERNETES_SERVICE_HOST") != "" &&
		os.Getenv("KUBERNETES_SERVICE_PORT") != "" &&
		err == nil && !fi.IsDir()
}
```

すなわち、トークンファイルが存在し (ディレクトリではなく)、かつ
`KUBERNETES_SERVICE_HOST` と `KUBERNETES_SERVICE_PORT` がいずれも空でないことである。

**この条件が妥当である根拠**: 環境変数が無ければ問い合わせ先が存在しないため、
トークンファイルがあっても資格情報として使えない。トークンファイルの有無だけで
判定すると、手元の端末に何らかの理由でこのファイルが置かれているだけで、
`~/.kube/config` を持つ利用者の挙動が変わる。

**D3 との関係**: 選択を client-go へ委ねる (D3) ため、本パッケージがこの判定を
自前で行う場面は 1 つだけである。**名前空間の導出元を決めるとき**に、選ばれたのが
設定ファイルかクラスタ内の資格情報かを知る必要がある (D4)。そこでは
`Possible()` と同じ条件を用い、client-go の判断と食い違わないようにする。

---

## D6: `POD_NAMESPACE` を尊重しない

**決定**: 環境変数 `POD_NAMESPACE` を名前空間の導出元として扱わない。

**根拠**: client-go はクラスタ内の場合に `POD_NAMESPACE` を最優先で見る (D4 のコード)。
しかし FR-013 は導出元を「認証情報が持つ名前空間の値」と定め、クラスタ内の資格情報に
ついては `/var/run/secrets/kubernetes.io/serviceaccount/namespace` と明示している。
`POD_NAMESPACE` は Downward API で利用者が任意に注入する値であり、資格情報が持つ値では
ない。両者が食い違った場合、トークンを読む権限は資格情報の名前空間に紐づくため、
`POD_NAMESPACE` を優先すると権限不足で失敗する。

名前空間を変えたい利用者には `WithNamespace` がある。環境変数による暗黙の上書きを
別に用意すると、どちらが効いているのかを利用者が追えない (憲章 原則 V の
「既定値は保守的に、変更手段は Option で」)。

---

## D7: Secret の値の復号

**決定**: `corev1.Secret.Data` (`map[string][]byte`) を読む。`StringData` は読まない。
自前の base64 復号は行わない。

**根拠**: Kubernetes API の Secret は `data` を base64 で表現するが、Go の型は
`map[string][]byte` であり、JSON のデコード時に標準の `encoding/json` が base64 を
復号する。したがって `Data[key]` はすでに復号済みのバイト列である。FR-018 は
この経路で自動的に満たされる。

`StringData` は書き込み専用 (write-only) のフィールドであり、API サーバは応答に含めない。
読み出し側で参照すると常に空になるため、参照しない。

**検証で固定すべき点**: 「符号化された文字列がそのまま返らない」(受け入れシナリオ 1-2、
SC-011) は、`Data` を使っていれば自明に成立する。テストでは `Data` に生の値を置いた
Secret を返す差し替えで確認する。base64 の二重復号を書いていないことの確認でもある。

---

## D8: エラーの分類

**決定**: `k8s.io/apimachinery/pkg/api/errors` の判定関数で API サーバの応答を分類し、
本パッケージの番兵エラーへ対応づける。元のエラーは `%w` で包んで残す。

| 判定 | 本パッケージのエラー | 対応する要件 |
|---|---|---|
| `errors.IsNotFound(err)` | `ErrSecretNotFound` | FR-024 |
| `errors.IsForbidden(err)` / `errors.IsUnauthorized(err)` | `ErrForbidden` | FR-027 |
| 上記以外の失敗 | 包んで返す (番兵なし) | FR-028 |
| `Data` にキーが無い | `ErrKeyNotFound` | FR-025 |
| 値の長さが 0 | `ErrEmptySecret` | FR-026 |

**根拠**: 4 つの原因は利用者の対処が異なる (Secret の作成 / キーの追加 / 値の投入 /
権限の付与)。`errors.Is` で判別できる形にすることは既存の 4 モジュールの作法でもある
(`misc/azure` の `ErrEmptySecret` / `ErrKeyNotFound`)。

`IsUnauthorized` を `ErrForbidden` に寄せるのは、どちらも利用者の対処が「権限の設定」で
あり、401 と 403 の区別が対処を変えないためである。元のエラーは包んであるので、
区別が必要な利用者は取り出せる。

**FR-029 の担保**: 番兵エラーの文言に値を含めない。`Data` の中身をエラーへ入れる
経路を作らない。キー名と名前空間・Secret 名は含めてよい (値ではないため)。

---

## D9: テストの継ぎ目

**決定**: 1 メソッドの自前インターフェースを公開する。

```go
type SecretsAPI interface {
	Get(ctx context.Context, namespace, name string, opts metav1.GetOptions) (*corev1.Secret, error)
}
```

`*kubernetes.Clientset` から本インターフェースへの薄いアダプタを同梱する。
単体テストは手書きの差し替えを使う。

**根拠**: 既存の 4 モジュールがいずれも自前の 1〜数メソッドのインターフェースを公開して
差し替え可能にしている (`misc/aws` の `SecretsManagerAPI`、`misc/azure` の `SecretsAPI`、
`misc/gcp` の `SecretManagerAPI`)。同じ作法に揃える。

名前空間をメソッドの引数に置くのは、client-go の `CoreV1().Secrets(ns).Get(...)` という
2 段の形をそのまま写すと、差し替えが 2 段になり、テストが本質でない構造の模倣に
なるためである。名前空間の決定は本パッケージの責務 (FR-013・FR-014) であり、
引数として渡す形が責務の所在と一致する。

**検討した代替案**: `k8s.io/client-go/kubernetes/fake` の fake clientset を使う案。
公式の fake は網羅的で信頼できるが、(a) 継ぎ目が client-go の型に固定され、利用者が
自分の実装を差し込めなくなる、(b) 既存 4 モジュールの作法から外れる、
(c) テスト専用の依存が増える。手書きの差し替えで足りる規模 (メソッド 1 つ) である。

---

## D10: 公開 API の形

**決定**: コンストラクタを 2 つ公開する。

| 関数 | 用途 | 名前空間の決定 |
|---|---|---|
| `NewTokenProvider(client SecretsAPI, opts ...Option)` | 利用者が接続情報を明示する (FR-019) | `WithNamespace` が必須。無ければ生成時にエラー |
| `NewTokenProviderFromEnvironment(opts ...Option)` | 接続情報を自動選択する (FR-020) | 選ばれた認証情報から導出 (FR-013)。導けなければエラー (FR-014) |

**根拠**: 既存の 4 モジュールは `NewTokenProvider(client, <位置>, opts...)` の形で、
クライアントを必須の引数に取る (004 の FR-008・FR-014)。`NewTokenProvider` はこの作法を
そのまま引き継ぐ。Kubernetes 固有の自動選択は別の関数として足す。1 つの関数で
`client == nil` のときだけ自動選択に切り替える設計は、引数の値で挙動が変わる分岐を
利用者に覚えさせるため採らない。

クライアントを注入した経路で `WithNamespace` を必須にするのは、注入されたクライアントが
名前空間の値を持たないためである。FR-014 の「認証情報から名前空間の値が得られない場合は
エラー」がそのまま適用される。

**名前について**: `FromEnvironment` は環境変数だけでなく、実行環境 (環境変数と 2 つの
ファイル) から導くことを指す。`FromEnv` は環境変数のみを想起させ、
`utils.TokenFromEnv` (環境変数 1 本を読む既存の関数) と紛れるため避けた。

**公開しないもの**: 選択された `*rest.Config` を返す探索関数は公開しない。
必要になれば後から足せるが、先に公開すると `rest.Config` が本モジュールの公開 API の
一部になり、client-go の版を上げにくくなる。

---

## D11: リポジトリ側で追随が必要な箇所

**決定**: 次の 4 点を実装に含める。Makefile と `.openapi-generator-ignore` は変更不要。

| 箇所 | 現状 | 対応 |
|---|---|---|
| `Makefile` の `MODULES` | `find` で `go.mod` の位置から導出している (specs/001 で導出化済み) | **変更不要。** `misc/k8s/go.mod` を置けば全ゲートの対象に自動で入る (FR-005、SC-016) |
| `.openapi-generator-ignore` | 37 行目に `misc/**` がある | **変更不要。** 新しい手書きファイルは既に保護対象 (憲章 原則 I) |
| `README.md` | 33-36 行に 4 モジュールの `go get`、88-91 行に一覧表、186 行に「計 5 モジュール」 | `misc/k8s` の行を 2 か所へ追加し、186 行を「計 6 モジュール」へ (FR-036) |
| `.github/workflows/sbom.yml` | 6 行目のコメントが「root と misc/* の計 5 つの go.mod」 | 「計 6 つ」へ改める。SBOM の生成自体はディレクトリ全体を走査しており、モジュールを列挙していないため**コメント以外の変更は不要** |
| `doc.go` / `utils/doc.go` | 4 モジュールを列挙している (`doc.go` 103 行、`utils/doc.go` 67-70 行) | `misc/k8s` を追加 (FR-036、憲章 原則 III) |
| `CHANGELOG.md` | `[Unreleased]` の `Added` がある | 本機能を追記 (FR-036、憲章 原則 III) |

**憲章側の追随 (本機能の範囲外)**: 憲章 原則 II が「現在の実数は 5 件 (本体と `misc/`
配下の 4 つ) である」と書いている。モジュールが 6 件になるため、この**参考値**が
古くなる。規範は「`go.mod` の位置から導出する」であって固定の数ではないため違反では
ないが、憲章の PATCH 改訂 (意味を変えない数値の更新) が望ましい。実装とは別の
Pull Request とする。
