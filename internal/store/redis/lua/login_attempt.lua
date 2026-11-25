-- Records a login attempt and returns the remaining lockout in milliseconds
-- (0 if the attempt is allowed).
--
-- KEYS[1] - attempt counter key
-- KEYS[2] - lock key
-- ARGV[1] - counter window, ms
-- ARGV[2] - number of free attempts
-- ARGV[3..] - backoff steps, ms (the last one is the ceiling)
local lock_ttl = redis.call('PTTL', KEYS[2])
if lock_ttl > 0 then
	return lock_ttl
end

local n = redis.call('INCR', KEYS[1])
redis.call('PEXPIRE', KEYS[1], ARGV[1])

local free = tonumber(ARGV[2])
local steps = #ARGV - 2
local idx = n - free
if idx >= 1 then
	if idx > steps then idx = steps end
	redis.call('SET', KEYS[2], '1', 'PX', tonumber(ARGV[2 + idx]))
end

return 0
