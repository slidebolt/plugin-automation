package snippets

// Builtins returns the built-in reusable script sources provided by
// plugin-automation. These are consumed by the gateway runtime.
func Builtins() map[string]string {
	return map[string]string{
		"Christmas": christmasSource,
	}
}

const christmasSource = `
local state = This.LoadSession()
local index = 0
if state ~= nil and state.index ~= nil then
  index = state.index
end

This.SendEvent({type = "christmas_started"})

local colors = {
  {255, 0, 0},
  {0, 255, 0},
  {255, 180, 120},
}

local length = 0
if Strip ~= nil and Strip.Length ~= nil then
  length = Strip.Length()
end
if length <= 0 then
  length = 1
end

Strip.On()
Strip.SetBrightness(100)

local timer = TimerService.Scripting.Every(0.25, function()
  local color = colors[(index % #colors) + 1]
  Strip.SetSegment(index % length, color)
  index = (index + 1) % length
  This.SaveSession({index = index})
end)
`
