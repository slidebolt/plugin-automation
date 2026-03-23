Automation("theme-christmas", {
  trigger = Interval({min=1.0, max=2.0}),
  targets = None(),
}, function(ctx)
  local colors = {
    {255, 0, 0},
    {0, 128, 0},
    {255, 215, 0},
    {255, 255, 255},
  }
  local i = 0
  ctx.targets:each(function(e)
    if e.type == "light" then
      i = i + 1
      local c = colors[((i - 1) % #colors) + 1]
      ctx.send(e, "light_set_rgb", {r=c[1], g=c[2], b=c[3], brightness=200})
    end
  end)
end)
