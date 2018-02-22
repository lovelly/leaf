package gameError

import (
	"fmt"

	"github.com/pkg/errors"
)

type ErrCode struct {
	ErrorCode      int
	DescribeString string
}


func (err *ErrCode) ToSysError() error {
	if err == nil {
		return nil
	}
	return errors.New(err.DescribeString)
}

func RenderError(code int, Desc ...string) *ErrCode {
	var des string
	if len(Desc) < 1 {
		des = fmt.Sprintf("请求错误, 错误码: %d", code)
	} else {
		des = fmt.Sprintf(Desc[0]+", 错误码: %d", code)
	}
	return &ErrCode{
		ErrorCode:      code,
		DescribeString: des,
	}
}

func PanicError(code int, Desc ...string) {
	var des string
	if len(Desc) < 1 {
		des = fmt.Sprintf("请求错误, 错误码: %d", code)
	} else {
		des = fmt.Sprintf(Desc[0]+", 错误码: %d", code)
	}
	panic(&ErrCode{
		ErrorCode:      code,
		DescribeString: des,
	})
}

func RenderServerError(Desc string) *ErrCode {
	return &ErrCode{
		ErrorCode:      0xFFFFFFFF,
		DescribeString: Desc,
	}
}

type writer interface {
	WriteError(id string, err *ErrCode)
}

func WriteError(w writer, id string, code int, Desc ...string) {
	var des string
	if len(Desc) < 1 {
		des = fmt.Sprintf("请求错误, 错误码: %d", code)
	} else {
		des = fmt.Sprintf(Desc[0]+", 错误码: %d", code)
	}
	err := &ErrCode{
		ErrorCode:      code,
		DescribeString: des,
	}
	w.WriteError(id, err)
}
