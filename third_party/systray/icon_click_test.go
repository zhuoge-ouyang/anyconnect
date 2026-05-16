package systray

import "testing"

func TestDispatchIconClickReportsWhetherHandlerWasInstalled(t *testing.T) {
	SetIconClickHandler(nil)
	if dispatchIconClick(true) {
		t.Fatal("dispatchIconClick returned handled without a handler")
	}

	var got bool
	SetIconClickHandler(func(left bool) {
		got = left
	})
	if !dispatchIconClick(true) {
		t.Fatal("dispatchIconClick did not report handled")
	}
	if !got {
		t.Fatal("dispatchIconClick did not pass left=true")
	}
	SetIconClickHandler(nil)
}
