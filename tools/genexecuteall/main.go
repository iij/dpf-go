// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	defaultLimit = 10000
	logsLimit    = 100
	outputFile   = "executeall_gen.go"
	specFile     = "openapi.json"
)

// pageable は ExecuteAll を生成する対象の 1 リクエストを表す。
type pageable struct {
	requestType string // 例: ApiGetZoneListRequest
	returnType  string // 例: GetZones（先頭の * は除く）
	operationId string // 例: getZoneList
	isLog       bool
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "genexecuteall:", err)
		os.Exit(1)
	}
}

func run() error {
	logOps, err := loadLogOperationIds(specFile)
	if err != nil {
		return fmt.Errorf("openapi.json の解析に失敗しました: %w", err)
	}

	apiFiles, err := filepath.Glob("api_*.go")
	if err != nil {
		return err
	}

	fset := token.NewFileSet()

	// limit/offset を持つリクエスト構造体, Execute() の戻り値, model の results
	// フィールド名を収集する。
	hasLimitOffset := map[string]bool{}
	execReturn := map[string]string{}   // requestType -> returnType
	resultsField := map[string]string{} // modelType -> results フィールド名

	for _, f := range append(apiFiles, mustGlob("model_*.go")...) {
		file, err := parser.ParseFile(fset, f, nil, 0)
		if err != nil {
			return fmt.Errorf("%s: %w", f, err)
		}
		collectStructs(file, hasLimitOffset, resultsField)
		collectExecutes(file, execReturn)
	}

	var targets []pageable
	for reqType := range hasLimitOffset {
		ret, ok := execReturn[reqType]
		if !ok {
			continue // Execute() が無い（通常あり得ない）
		}
		if _, ok := resultsField[ret]; !ok {
			// 戻り値に results 配列が無い場合は Pager ではない。
			fmt.Fprintf(os.Stderr, "genexecuteall: %s は results フィールドを持たないためスキップ\n", ret)
			continue
		}
		opId := operationIdFromRequest(reqType)
		targets = append(targets, pageable{
			requestType: reqType,
			returnType:  ret,
			operationId: opId,
			isLog:       logOps[opId],
		})
	}

	sort.Slice(targets, func(i, j int) bool { return targets[i].requestType < targets[j].requestType })

	src, err := render(targets, resultsField)
	if err != nil {
		return err
	}
	if err := os.WriteFile(outputFile, src, 0o644); err != nil {
		return err
	}
	fmt.Printf("genexecuteall: %d 個の ExecuteAll() を %s に生成しました\n", len(targets), outputFile)
	return nil
}

func mustGlob(pat string) []string {
	m, _ := filepath.Glob(pat)
	return m
}

// collectStructs は struct 定義を走査し、limit/offset を両方持つリクエスト型と、
// json:"results" タグを持つ model のフィールド名を収集する。
func collectStructs(file *ast.File, hasLimitOffset map[string]bool, resultsField map[string]string) {
	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		for _, spec := range gd.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}
			st, ok := ts.Type.(*ast.StructType)
			if !ok {
				continue
			}
			name := ts.Name.Name

			var hasLimit, hasOffset bool
			for _, fld := range st.Fields.List {
				for _, n := range fld.Names {
					switch n.Name {
					case "limit":
						hasLimit = true
					case "offset":
						hasOffset = true
					}
				}
				// model の results フィールド名をタグから検出する。
				if fld.Tag != nil && strings.Contains(fld.Tag.Value, `json:"results`) {
					if len(fld.Names) > 0 {
						resultsField[name] = fld.Names[0].Name
					}
				}
			}
			if hasLimit && hasOffset && strings.HasPrefix(name, "Api") && strings.HasSuffix(name, "Request") {
				hasLimitOffset[name] = true
			}
		}
	}
}

// collectExecutes は Execute() メソッドを走査し、レシーバ型 -> 戻り値型を収集する。
func collectExecutes(file *ast.File, execReturn map[string]string) {
	for _, decl := range file.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok || fd.Name.Name != "Execute" || fd.Recv == nil || len(fd.Recv.List) != 1 {
			continue
		}
		recv := exprName(fd.Recv.List[0].Type)
		if recv == "" || fd.Type.Results == nil || len(fd.Type.Results.List) == 0 {
			continue
		}
		ret := exprName(fd.Type.Results.List[0].Type)
		ret = strings.TrimPrefix(ret, "*")
		execReturn[recv] = ret
	}
}

// exprName は型式から（ポインタ * 付きの）型名を取り出す。
func exprName(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return "*" + exprName(t.X)
	case *ast.SelectorExpr:
		return exprName(t.X) + "." + t.Sel.Name
	}
	return ""
}

// operationIdFromRequest は ApiGetZoneListRequest -> getZoneList に変換する。
func operationIdFromRequest(reqType string) string {
	s := strings.TrimPrefix(reqType, "Api")
	s = strings.TrimSuffix(s, "Request")
	if s == "" {
		return s
	}
	return strings.ToLower(s[:1]) + s[1:]
}

// loadLogOperationIds は openapi.json を解析し、limit パラメータが
// SearchLogsLimit を参照する operationId の集合を返す。
func loadLogOperationIds(path string) (map[string]bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var spec struct {
		Paths      map[string]map[string]json.RawMessage `json:"paths"`
		Components struct {
			Parameters map[string]struct {
				Schema struct {
					Ref string `json:"$ref"`
				} `json:"schema"`
			} `json:"parameters"`
		} `json:"components"`
	}
	if err := json.Unmarshal(data, &spec); err != nil {
		return nil, err
	}

	// limit パラメータ名 -> SearchLogsLimit を参照するか
	logLimitParam := map[string]bool{}
	for name, p := range spec.Components.Parameters {
		if strings.HasSuffix(p.Schema.Ref, "/SearchLogsLimit") {
			logLimitParam[name] = true
		}
	}

	result := map[string]bool{}
	for _, methods := range spec.Paths {
		for method, raw := range methods {
			switch method {
			case "get", "post", "put", "patch", "delete":
			default:
				continue
			}
			var op struct {
				OperationId string `json:"operationId"`
				Parameters  []struct {
					Ref string `json:"$ref"`
				} `json:"parameters"`
			}
			if err := json.Unmarshal(raw, &op); err != nil {
				continue
			}
			for _, pr := range op.Parameters {
				name := pr.Ref[strings.LastIndex(pr.Ref, "/")+1:]
				if logLimitParam[name] {
					result[op.OperationId] = true
				}
			}
		}
	}
	return result, nil
}

func render(targets []pageable, resultsField map[string]string) ([]byte, error) {
	var b bytes.Buffer
	b.WriteString("// SPDX-License-Identifier: Apache-2.0\n\n")
	b.WriteString("// Code generated by tools/genexecuteall. DO NOT EDIT.\n\n")
	b.WriteString("package dpf\n\n")
	b.WriteString("import \"net/http\"\n\n")

	for _, t := range targets {
		limit := defaultLimit
		kind := "通常の一覧"
		if t.isLog {
			limit = logsLimit
			kind = "log 系"
		}
		field := resultsField[t.returnType]

		fmt.Fprintf(&b, "// ExecuteAll は Pager である Execute() を全ページ分繰り返し呼び出し、\n")
		fmt.Fprintf(&b, "// すべての結果(%s)を結合した *%s を返す。\n", field, t.returnType)
		fmt.Fprintf(&b, "// 1ページあたりの取得件数(limit)は %d（%s API）。\n", limit, kind)
		fmt.Fprintf(&b, "// 途中でエラーが発生した場合は、それまでに取得した結果とエラーを返す。\n")
		fmt.Fprintf(&b, "func (r %s) ExecuteAll() (*%s, *http.Response, error) {\n", t.requestType, t.returnType)
		fmt.Fprintf(&b, "\tconst pageSize = int32(%d)\n", limit)
		fmt.Fprintf(&b, "\tvar all *%s\n", t.returnType)
		b.WriteString("\tvar resp *http.Response\n")
		b.WriteString("\tfor offset := int32(0); ; offset += pageSize {\n")
		b.WriteString("\t\to := offset\n")
		b.WriteString("\t\tl := pageSize\n")
		b.WriteString("\t\tr.offset = &o\n")
		b.WriteString("\t\tr.limit = &l\n")
		b.WriteString("\t\tpage, pageResp, err := r.Execute()\n")
		b.WriteString("\t\tresp = pageResp\n")
		b.WriteString("\t\tif err != nil {\n")
		b.WriteString("\t\t\treturn all, resp, err\n")
		b.WriteString("\t\t}\n")
		b.WriteString("\t\tif page == nil {\n")
		b.WriteString("\t\t\tbreak\n")
		b.WriteString("\t\t}\n")
		b.WriteString("\t\tif all == nil {\n")
		b.WriteString("\t\t\tall = page\n")
		b.WriteString("\t\t} else {\n")
		fmt.Fprintf(&b, "\t\t\tall.%s = append(all.%s, page.%s...)\n", field, field, field)
		b.WriteString("\t\t}\n")
		fmt.Fprintf(&b, "\t\tif int32(len(page.%s)) < pageSize {\n", field)
		b.WriteString("\t\t\tbreak\n")
		b.WriteString("\t\t}\n")
		b.WriteString("\t}\n")
		b.WriteString("\treturn all, resp, nil\n")
		b.WriteString("}\n\n")
	}

	return format.Source(b.Bytes())
}
