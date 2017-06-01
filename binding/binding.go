// Copyright 2014 Manu Martinez-Almeida.  All rights reserved.
// Use of this source code is governed by a MIT style
// license that can be found in the LICENSE file.

package binding

import "net/http"

//liujia: 主要要注意的就是json xml form multiform protobuf
const (
	MIMEJSON              = "application/json"
	MIMEHTML              = "text/html"
	MIMEXML               = "application/xml"
	MIMEXML2              = "text/xml"
	MIMEPlain             = "text/plain"
	MIMEPOSTForm          = "application/x-www-form-urlencoded"
	MIMEMultipartPOSTForm = "multipart/form-data"
	MIMEPROTOBUF          = "application/x-protobuf"
)

//liujia: Binding是个接口，Name()返回名字，如JSON FORM等，Bind()将http.Request上的参数，bind到一个interface{}（通常是一个struct）上
type Binding interface {
	Name() string
	Bind(*http.Request, interface{}) error
}

type StructValidator interface {
	// ValidateStruct can receive any kind of type and it should never panic, even if the configuration is not right.
	// If the received type is not a struct, any validation should be skipped and nil must be returned.
	// If the received type is a struct or pointer to a struct, the validation should be performed.
	// If the struct is not valid or the validation itself fails, a descriptive error should be returned.
	// Otherwise nil must be returned.
	ValidateStruct(interface{}) error
}

var Validator StructValidator = &defaultValidator{}

//liujia: 当前实现的Binding接口的几种类型
var (
	JSON          = jsonBinding{}
	XML           = xmlBinding{}
	Form          = formBinding{}
	FormPost      = formPostBinding{}
	FormMultipart = formMultipartBinding{}
	ProtoBuf      = protobufBinding{}
)

//liujia: 根据请求类型和content-Type(应该是请求header里的)，返回特定类型的Binding接口
func Default(method, contentType string) Binding {
	if method == "GET" {
		return Form
	} else {
		switch contentType {
		case MIMEJSON:
			return JSON
		case MIMEXML, MIMEXML2:
			return XML
		case MIMEPROTOBUF:
			return ProtoBuf
		default: //case MIMEPOSTForm, MIMEMultipartPOSTForm:
			return Form
		}
	}
}

//liujia: 参考defaultValidator
func validate(obj interface{}) error {
	if Validator == nil {
		return nil
	}
	return Validator.ValidateStruct(obj)
}
