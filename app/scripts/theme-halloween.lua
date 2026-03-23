Automation("theme-halloween", {
  trigger = Interval({min=0.8, max=1.5}),
  targets = None(),
}, function(ctx)
  local colors = {
    {255, 100, 0},
    {128, 0, 128},
    {0, 255, 0},
    {255, 50, 0},
  }
  local i = 0
  ctx.targets:each(function(e)
    if e.type == "light" then
      i = i + 1
      local c = colors[((i - 1) % #colors) + 1]
      ctx.send(e, "light_set_rgb", {r=c[1], g=c[2], b=c[3], brightness=150})
    end
  end)
end)
