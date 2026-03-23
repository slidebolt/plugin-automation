Automation("theme-easter", {
  trigger = Interval({min=2.0, max=3.0}),
  targets = None(),
}, function(ctx)
  local colors = {
    {255, 182, 193},
    {173, 216, 230},
    {255, 255, 150},
    {200, 162, 255},
    {144, 238, 144},
  }
  local i = 0
  ctx.targets:each(function(e)
    if e.type == "light" then
      i = i + 1
      local c = colors[((i - 1) % #colors) + 1]
      ctx.send(e, "light_set_rgb", {r=c[1], g=c[2], b=c[3], brightness=180})
    end
  end)
end)
