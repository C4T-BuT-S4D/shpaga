package monitor

import (
	"strings"
)

type CallbackAction string

const (
	CallbackActionNewMemberAccept CallbackAction = "new_member_accept"
	CallbackActionNewMemberKick   CallbackAction = "new_member_kick"
)

func (a CallbackAction) String() string {
	return string(a)
}

func (a CallbackAction) DataMatches(data string) bool {
	cringePrefix := "\f" + a.String()
	return data == cringePrefix || strings.HasPrefix(data, cringePrefix+"|")
}
