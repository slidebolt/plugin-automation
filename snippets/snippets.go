package snippets

// Builtins returns the built-in reusable script sources provided by
// plugin-automation. These are consumed by the gateway runtime.
func Builtins() map[string]string {
	return map[string]string{
		"Christmas":        christmasSource,
		"LightStripScenes": lightStripScenesHost,
		"Scenes":           scenesSource,
	}
}

const lightStripScenesHost = `
-- LightStripScenes Host Script
-- This script runs on the LightStripScenes entity and manages child scenes.

local active_pid = nil

function OnCommand(cmd)
  if cmd.type == "run_scene" then
    if active_pid ~= nil then
      This.StopScript(active_pid)
    end
    active_pid = This.RunScript("Scenes." .. cmd.name)
  elseif cmd.type == "stop_scene" then
    if active_pid ~= nil then
      This.StopScript(active_pid)
      active_pid = nil
    end
  end
end
`

const scenesSource = `
DefineScript("Rainbow", function()
  local colors = {
    {255, 0, 0}, {255, 127, 0}, {255, 255, 0}, {0, 255, 0}, {0, 0, 255}, {75, 0, 130}, {148, 0, 211}
  }
  local length = Strip.Length()
  local index = 0
  TimerService.Scripting.Every(0.1, function()
    for i = 0, length - 1 do
      local c = colors[((i + index) % #colors) + 1]
      Strip.SetSegment(i, c)
    end
    index = (index + 1) % #colors
  end)
end)

DefineScript("Fire", function()
  local length = Strip.Length()
  TimerService.Scripting.Every(0.05, function()
    for i = 0, length - 1 do
      local r = 200 + math.random(55)
      local g = 50 + math.random(100)
      Strip.SetSegment(i, {r, g, 0})
    end
  end)
end)
`

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
