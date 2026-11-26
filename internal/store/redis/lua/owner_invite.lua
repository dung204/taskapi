-- Rate-limits invitations sent by one owner and returns how long to wait in
-- milliseconds (0 if the invitation is allowed).
--
-- KEYS[1] - hourly counter key
-- ARGV[1] - max invitations per window
-- ARGV[2] - window, ms
local n = redis.call('INCR', KEYS[1])
if n == 1 then
	redis.call('PEXPIRE', KEYS[1], tonumber(ARGV[2]))
end
if n > tonumber(ARGV[1]) then
	local left = redis.call('PTTL', KEYS[1])
	if left < 1 then left = 1 end
	return left
end
return 0
