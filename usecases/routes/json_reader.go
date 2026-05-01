package routes

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"time"
)

type JSONReader struct {
	rawMessage json.RawMessage
}

func (r JSONReader) String() string {
	var output string
	json.Unmarshal(r.rawMessage, &output)
	return output
}

func NewJSONReader(bytes []byte) *JSONReader {
	return &JSONReader{rawMessage: json.RawMessage(bytes)}
}

func NewJSONReaderFromFile(path string) (*JSONReader, error) {
	readResult, err := os.ReadFile(path)

	if err != nil {
		return nil, err
	}
	return NewJSONReader(readResult), nil
}

func (reader *JSONReader) Unmarshal(item any) error {
	return json.Unmarshal(reader.rawMessage, item)
}

func (reader *JSONReader) UnmarshalIntoArray() ([]*JSONReader, error) {

	array, err := rawMessageToArray(reader.rawMessage)
	if err != nil {
		return nil, err
	}
	output := []*JSONReader{}
	for _, item := range array {
		output = append(output, NewJSONReader(item))
	}
	return output, nil
}

func (reader *JSONReader) IntoString() string {
	return string(reader.rawMessage)
}
func (reader *JSONReader) Bytes() []byte {
	return []byte(strings.Trim(string(reader.rawMessage), "\" "))
}

func (reader *JSONReader) UnmarshalIntoMap() (map[string]*JSONReader, error) {

	array, err := rawMessageToMap(reader.rawMessage)
	if err != nil {
		return nil, err
	}
	output := map[string]*JSONReader{}
	for key, item := range array {
		output[key] = NewJSONReader(item)
	}
	return output, nil
}

func rawMessageToArray(message json.RawMessage) ([]json.RawMessage, error) {
	output := []json.RawMessage{}
	return output, json.Unmarshal(message, &output)
}

func rawMessageToMap(message json.RawMessage) (map[string]json.RawMessage, error) {
	output := map[string]json.RawMessage{}
	return output, json.Unmarshal(message, &output)

}

type JSONType string

const (
	LIST      JSONType = "LIST"
	MAP       JSONType = "MAP"
	NUMBER    JSONType = "NUMBER"
	BOOL      JSONType = "BOOL"
	STRING    JSONType = "STRING"
	UNDEFINED JSONType = "UNDEFINED"
)

func (reader *JSONReader) Type() JSONType {
	for _, char := range reader.rawMessage {
		switch char {
		case 'f', 't':
			return BOOL

		case '[':
			return LIST

		case '{':
			return MAP

		case '"':
			return STRING

		case '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
			return NUMBER
		}
	}

	return UNDEFINED

}
func (reader *JSONReader) Get(path string) (*JSONReader, error) {

	path = strings.TrimSpace(path)

	if path == "" {
		return reader, nil
	}

	if !strings.HasPrefix(path, "[") {
		return nil, fmt.Errorf("invalid Path error! Keys and values must be inside backets: %v", path)
	}

	var data json.RawMessage
	var finalErr error

	func() {

		defer func() {
			if err := recover(); err != nil {
				finalErr = fmt.Errorf("%v", err)
			}
		}()

		pathArray := strings.Split(strings.ReplaceAll(strings.TrimSpace(path), "][", "] *** ["), " *** ")

		temp := make(json.RawMessage, len(reader.rawMessage))
		copy(temp, reader.rawMessage)

		for _, key := range pathArray {

			key = strings.TrimSpace(key)[1 : len(key)-1]

			index, err := strconv.Atoi(key)
			isArray := err == nil
			if !isArray && key == "?" {
				isArray = true
			}

			if isArray {

				output, err := rawMessageToArray(temp)
				if err != nil {
					finalErr = err
					return
				}

				currentLength := len(output)
				index = GetRandomNumber(currentLength)

				if index < 0 {
					index = len(output) + index
				}

				temp = output[index]

			} else {

				key = strings.TrimSpace(strings.ReplaceAll(key, "\"", ""))

				output, err := rawMessageToMap(temp)
				if err != nil {
					finalErr = err
					return
				}
				temp = output[key]
			}
		}
		data = temp

	}()

	return NewJSONReader(data), finalErr
}

var rng = rand.New(rand.NewSource(time.Now().UnixNano()))

func GetRandomNumber(n int) int {
	return rng.Intn(n)

}
