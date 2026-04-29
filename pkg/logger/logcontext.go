package logger

import (
	"encoding/json"
	"fmt"
	"easyserver/pkg/common"
	"strings"
)

type LogContext map[string]string

func (c LogContext) String() string {
	bytes, _ := json.Marshal(c)
	return string(bytes)
}

func (c LogContext) Add(key, value string) LogContext {

	c[key] = value
	return c
}

func (c LogContext) Combine(other LogContext) LogContext {
	for key, value := range other {
		c[key] = value
	}
	return c
}

func NewLogContext() LogContext {
	c := LogContext{}
	return c
}

func (c LogContext) LogWithDebug(messages ...string) {
	Debugf("\n%v[message]:: %v\n\n", c, processMessages(messages))
}

func (c LogContext) LogWithTrace(messages ...string) {
	Tracef("\n%v[message]:: %v\n\n", c, processMessages(messages))
}

func (c LogContext) LogWithWarn(messages ...string) {
	Warnf("\n%v[message]:: %v\n\n", c, processMessages(messages))
}

func (c LogContext) LogWithFatal(messages ...string) {
	Fatalf("\n%v[message]:: %v\n\n", c, processMessages(messages))
}

func (c LogContext) LogWithInfo(messages ...string) {
	Infof("\n%v[message]:: %v\n\n", c, processMessages(messages))
}

func (c LogContext) LogWithError(messages ...string) {
	fmt.Println(string(common.GetStackTrace()))
	Errorf("\n%v[message]:: %v\n\n", c, processMessages(messages))
}

func processMessages(messages []string) string {
	return strings.Join(messages, " ")
}
