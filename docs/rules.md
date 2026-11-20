# Detection rules (each needs 1 positive + 1 benign fixture, deterministic severity, evidence event IDs)
| Rule | Condition | Result |
|---|---|---|
| DET-001 | 6 failed logins for one identity within 5 minutes | High alert, ONE deduplicated incident |
| DET-002 | Unexpected new privileged group membership | High alert with event evidence |
| DET-003 | 10 failed connections to multiple ports from one source | Medium scan-pattern alert |
| DET-004 | Executable hash matches local lab IOC list (simulations/ioc_hashes.txt) | High alert, hash referenced |
| DET-005 | Agent heartbeat missing > 3 minutes | Medium agent-health alert |

Fixtures: simulations/*.json (DET-001..003 done; DET-004/005 to add).
These are defensive test signals, not proof of real intrusion.
