// SPDX-License-Identifier: Apache-2.0

// Command genexecuteall は、生成済みの api_*.go を解析し、Pager である
// Execute() を持つ（limit/offset を持つ）リクエストに対して、全ページを
// 取得する ExecuteAll() を executeall_gen.go に生成する。
//
// 1ページあたりの取得件数(limit)は、通常の一覧 API は 10000、log 系 API は
// 100 とする。log 系かどうかは openapi.json の limit パラメータが
// SearchLogsLimit を参照しているかで判定する。
package main
