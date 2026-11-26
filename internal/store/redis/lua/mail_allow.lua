-- Rate-limits mail requests per address and returns how long to wait in
-- milliseconds (0 if the request is allowed).
--
-- KEYS[1] - cooldown key
-- KEYS[2] - hourly counter key
-- ARGV[1] - max requests per window
-- ARGV[2] - cooldown, ms
-- ARGV[3] - window, ms
local cooldownLeft = redis.call('PTTL', KEYS[1])
if cooldownLeft > 0 then
	return cooldownLeft
end

local n = redis.call('INCR', KEYS[2])
if n == 1 then
	redis.call('PEXPIRE', KEYS[2], tonumber(ARGV[3]))
end

if n > tonumber(ARGV[1]) then
	local left = redis.call('PTTL', KEYS[2])
	if left < 1 then
		left = 1
	end
	return left
end

redis.call('SET', KEYS[1], '1', 'PX', tonumber(ARGV[2]))
return 0
