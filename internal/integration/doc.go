// SPDX-License-Identifier: Apache-2.0

// Package integration は実際の DPF-API と権威 DNS サーバを使う統合テストを収める。
//
// テスト本体はすべて "integration" ビルドタグを付けたテストファイルにあり、
// 通常の go test ./... では一切実行されない。実行方法と必要な環境変数は
// 本ディレクトリの README.md を参照。
//
//	make test-integration
//
// 本ファイルはタグ無しの Go ファイルが 1 つも無い状態を避けるために存在する。
// タグ付きファイルしか無いと go build ./... が
// "build constraints exclude all Go files" で失敗するため。
package integration
