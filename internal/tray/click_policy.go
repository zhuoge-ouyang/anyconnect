package tray

type IconButton int

const (
	IconButtonLeft IconButton = iota + 1
	IconButtonRight
)

type IconClickAction int

const (
	IconClickIgnore IconClickAction = iota
	IconClickOpenDashboard
	IconClickShowMenu
)

func ResolveIconClickAction(button IconButton, hasDashboardHandler bool) IconClickAction {
	switch button {
	case IconButtonLeft:
		if hasDashboardHandler {
			return IconClickOpenDashboard
		}
		return IconClickShowMenu
	case IconButtonRight:
		return IconClickIgnore
	default:
		return IconClickIgnore
	}
}
