# Analyze ZabKiss Addon Logs

Fetch the last 3000 lines of the ZabKiss addon logs from the production server and analyze them for problems. Do NOT modify any code.

## Steps

1. Fetch logs via SSH:
```
ssh root@192.168.2.148 'ha apps logs 0a184fd3_zabkiss --lines 3000 2>&1'
```

2. Analyze the logs and report:

### What to look for

**LLM behavior issues:**
- `status=reject` — commands the LLM refused; check if legitimate or wrong
- `status=ok` with `actions=N` where N>0 on a query (e.g. "какая температура?") — LLM executing commands when user asked a question
- LLM repeating the same action despite user corrections ("нет", "не то", "прежние")
- LLM answering state questions from history instead of sensor data (e.g. answers "123°C" when device shows "170°C")
- `end_session=true` on non-farewell commands — premature session close

**Service call issues:**
- `dispatch action err=...` — failed HA service calls
- `level=ERROR` lines — any errors

**Session / history issues:**
- Same session with `messages=N` growing abnormally large (>20)
- History accumulation without `end_session=true` causing stale context

**Auth issues:**
- `auth failed` — unexpected auth failures
- Users hitting account-linking unexpectedly

**Performance:**
- Requests taking close to the Alice timeout (Request-Timeout header vs response time)
- `context deadline exceeded`

### Output format

For each found issue:
- **Issue**: short description
- **Evidence**: the relevant log lines (timestamp + content)
- **Impact**: what the user actually experienced
- **Likely cause**: root cause hypothesis

End with a **Priority list** of issues to fix, ordered by user impact.
