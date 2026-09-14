// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/csv"
	"fmt"
	"io"
)

// record は go-licenses csv の 1 行に対応する。
type record struct {
	// Package はパッケージパスである。
	Package string
	// LicenseURL はライセンス本文の位置である。判別不能な場合は "Unknown"。
	LicenseURL string
	// LicenseID は SPDX 識別子である。判別不能な場合は "Unknown"。
	LicenseID string
}

// parseRecords は go-licenses csv の出力を読み込む。
//
// 列数が 3 でない行はエラーとする。無視すると、上流の出力形式が変わった
// ことを「違反なし」に見せてしまう。パッケージパスと識別子が空の行も
// エラーとする。空の識別子を判別不能として黙って通すと、判定の根拠が
// 分からなくなる（判別不能であることは "Unknown" が明示する）。
//
// 空行を飛ばす分岐は置かない。csv は空行を読み飛ばすため不要であり、
// 引用符だけの行（1 列で内容が空）を飛ばす副作用がある。それは列数の
// 異なる行であり、契約上は中断しなければならない。
//
// 入力が 1 行も無い場合もエラーとする。対象を取り違えて空の入力を
// 渡したことを「違反なし」として通さないため。
func parseRecords(r io.Reader) ([]record, error) {
	cr := csv.NewReader(r)
	// 列数の検査は自分で行い、エラーメッセージを具体的にする。
	cr.FieldsPerRecord = -1

	var out []record
	for line := 1; ; line++ {
		fields, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("%d 行目: 入力を読めませんでした: %w", line, err)
		}

		if len(fields) != 3 {
			return nil, fmt.Errorf("%d 行目: 3 列（パッケージ, URL, 識別子）である必要があります: %d 列でした", line, len(fields))
		}
		if fields[0] == "" {
			return nil, fmt.Errorf("%d 行目: パッケージパスが空です", line)
		}
		if fields[2] == "" {
			return nil, fmt.Errorf("%d 行目: ライセンス識別子が空です（判別不能なら Unknown になります）", line)
		}

		out = append(out, record{
			Package:    fields[0],
			LicenseURL: fields[1],
			LicenseID:  fields[2],
		})
	}

	if len(out) == 0 {
		return nil, fmt.Errorf("入力が空です（対象のモジュールを取り違えていないか確認してください）")
	}
	return out, nil
}
