package common

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

func WrapwithFunctionName(content string) string {
	pc, _, _, _ := runtime.Caller(1)
	functionName := runtime.FuncForPC(pc).Name()
	return fmt.Sprintf("%s\n[[%s]]", content, functionName)
}

func FromJSON[T any](content string, item *T) error {
	err := json.Unmarshal([]byte(content), item)
	return err
}

func PrintJSON(item any) {
	formattedJson, err := FormatObjectToJSONString(item)
	if err != nil {
		return
	}
	fmt.Println(string(formattedJson))
}

func Prettify(item any) string {
	formatted, _ := FormatObjectToJSONString(item)
	return formatted
}

func IsNil(v any) bool {
	if v == nil {
		return true
	}
	if reflect.TypeOf(v).Kind() != reflect.Ptr {
		return false
	}
	value := reflect.ValueOf(v)
	return !value.IsValid() || value.IsNil()
}

func EscapeQuotes(content string) string {

	bytes, err := JSONMarshal(content)
	output := ""
	if err != nil {
		output = strconv.Quote(content)
	} else {
		output = string(bytes)
	}
	return output[1 : len(output)-1]
}

func NormalizePath(path string) (string, error) {

	if !filepath.IsAbs(path) {
		cwd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		path = filepath.Join(cwd, path)
	}

	path = filepath.Clean(path)
	path, _ = strings.CutSuffix(path, string(os.PathSeparator))

	return path, nil
}

func FormatObjectToJSONString(item any) (string, error) {

	var jsonString string
	switch str := item.(type) {
	case string:
		jsonString = strings.TrimSpace(str)
	default:
		jsonString, _ = ToJSON(item)
	}

	var jsonData interface{}
	err := json.Unmarshal([]byte(jsonString), &jsonData)
	if err != nil {
		return "", err
	}
	formattedJson, err := json.MarshalIndent(jsonData, "", "  ")
	if err != nil {
		return "", err
	}
	return string(formattedJson), nil
}

func FormatJSONString(jsonString string) (string, error) {
	var jsonData interface{}
	err := json.Unmarshal([]byte(jsonString), &jsonData)
	if err != nil {
		return "", err
	}
	formattedJson, err := json.MarshalIndent(jsonData, "", "  ")
	if err != nil {
		return "", err
	}

	return string(formattedJson), nil
}

func JSONMarshal(item any) ([]byte, error) {
	// bytes, _ := JSONMarshal(v)
	builder := strings.Builder{}
	enc := json.NewEncoder(&builder)
	enc.SetEscapeHTML(false)
	err := enc.Encode(item)

	if err != nil {
		return nil, err
	}

	return []byte(builder.String()), nil
}

func ToJSON(item any) (string, error) {
	bytes, err := JSONMarshal(item)

	if err != nil {
		return "", err
	}

	return string(bytes), nil
}

func ToJSONString(v any) string {
	jsonString, _ := ToJSON(v)
	return jsonString
}

func PathJoin(paths ...string) string {
	if len(paths) == 0 {
		return ""
	}
	fullPath := path.Join(paths...)
	if strings.HasPrefix(paths[0], ".") {
		fullPath = "./" + fullPath
	}
	return fullPath
}

func ToJSONRawMessage(v any) json.RawMessage {
	bytes := ToJSONString(v)
	return json.RawMessage(bytes)
}

func LoadMapFromFile(path string) (Object, error) {
	obj := Object{}
	err := LoadObjectFromFile(path, obj)
	if err != nil {
		return nil, err
	}
	return obj, nil
}

func LoadObjectFromFile(path string, v any) error {

	bytes, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	ext := filepath.Ext(path)
	switch ext {
	case ".yml", ".yaml":
		return yaml.Unmarshal(bytes, v)
	case ".json":
		return json.Unmarshal(bytes, v)
	default:
		return fmt.Errorf("No loader for extension: %s", ext)
	}
}

func ToMap(item string) (map[string]any, error) {
	output := map[string]any{}
	err := json.Unmarshal([]byte(item), &output)
	return output, err
}

func ObjectToMap(item any) (map[string]any, error) {
	output := map[string]any{}
	bytes, err := JSONMarshal(item)
	if err != nil {
		return nil, err
	}
	err = json.Unmarshal(bytes, &output)
	return output, err
}

func PrintJSONString(jsonString string) {
	var jsonData interface{}
	err := json.Unmarshal([]byte(jsonString), &jsonData)
	if err != nil {
		return
	}
	formattedJson, err := json.MarshalIndent(jsonData, "", "  ")
	if err != nil {
		return
	}
	fmt.Println(string(formattedJson))
}

type Object = map[string]any

func PathExists(filename string) bool {
	_, err := os.Stat(filename)
	return err == nil //!os.IsNotExist(err)
}

func GetStackTrace() []byte {
	buf := make([]byte, 1024)
	for {
		n := runtime.Stack(buf, false)
		if len(buf) == 0 {
			break
		}
		if n < len(buf) {
			buf = buf[:n]
			break
		}
		buf = make([]byte, 2*len(buf))
	}
	return buf
	// log.Printf("Stack trace:\n%s", buf)
}

func CopyValue(src any, dest any) error {

	bytes, err := JSONMarshal(src)
	if err != nil {
		return err
	}

	return json.Unmarshal(bytes, dest)
}

func GetMapKeys[K comparable, V any](mapping map[K]V) []K {
	keys := []K{}
	for k := range mapping {
		keys = append(keys, k)
	}
	return keys
}

func UnmarshalJSONFromPath(path string, v any) error {

	bytes, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	return json.Unmarshal(bytes, v)
}

func Map[T any, K any](items []T, fn func(T) K) []K {
	output := []K{}
	for _, item := range items {
		output = append(output, fn(item))
	}
	return output
}

func Filter[T any](items []T, fn func(*T) bool) []T {
	output := []T{}
	for _, item := range items {
		if !fn(&item) {
			continue
		}
		output = append(output, item)
	}
	return output
}

func Each[T any](items []T, fn func(T)) {
	for _, item := range items {
		fn(item)
	}
}

func EachIndexed[T any](items []T, fn func(item T, i int)) {
	for i, item := range items {
		fn(item, i)
	}
}

func Reduce[T any, K any](items []T, init K, fn func(K, T) K) K {
	output := init
	for _, item := range items {
		output = fn(output, item)
	}
	return output
}

func Range[T any](count int, fn func(int) T) []T {
	output := []T{}
	for i := 0; i < count; i++ {
		output = append(output, fn(i))
	}
	return output
}

func Repeat[T any](count int, item T) []T {
	output := []T{}
	for i := 0; i < count; i++ {
		output = append(output, item)
	}
	return output
}
