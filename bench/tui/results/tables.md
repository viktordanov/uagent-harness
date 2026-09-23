### Binary size (bytes)

| spike | default build | -ldflags "-s -w" |
| --- | --- | --- |
| Bubble Tea v2 + bubbles viewport | 6.96 MB | 4.88 MB |
| Bubble Tea v2 + windowed transcript | 6.96 MB | 4.88 MB |
| tview (tcell v2) | 5.77 MB | 3.98 MB |
| tcell v3 raw | 4.76 MB | 3.17 MB |
| vaxis | 4.68 MB | 3.14 MB |
| ultraviolet direct (TerminalScreen) | 6.16 MB | 4.25 MB |
| awesome-gocui | 5.45 MB | 3.68 MB |

### Cold start (stripped binaries, 20 runs after 1 warm-up, pty 120x40)

| spike | first byte median ms | first full frame median ms | p10–p90 full ms | first full frame, silent terminal ms |
| --- | --- | --- | --- | --- |
| Bubble Tea v2 + bubbles viewport | 23.52 | 24.43 | 23.4–25.0 | 22 |
| Bubble Tea v2 + windowed transcript | 23.13 | 23.88 | 23.5–24.9 | 22 |
| tview (tcell v2) | 3.79 | 4.56 | 4.0–5.4 | 5 |
| tcell v3 raw | 3.05 | 3.35 | 3.1–3.7 | 1,008 |
| vaxis | 2.79 | 3.17 | 3.0–3.4 | 3,013 |
| ultraviolet direct (TerminalScreen) | 3.25 | 3.85 | 3.6–4.3 | 6 |
| awesome-gocui | 3.28 | 4.39 | 4.1–4.8 | 7 |

### Idle (5 s window starting 1 s after first frame)

| spike | CPU ms in 5 s | RSS KB | phys footprint KB |
| --- | --- | --- | --- |
| Bubble Tea v2 + bubbles viewport | 34.2 | 12,704 | 9,376 |
| Bubble Tea v2 + windowed transcript | 29.1 | 12,560 | 9,184 |
| tview (tcell v2) | 0 | 8,960 | 6,320 |
| tcell v3 raw | 0 | 7,920 | 5,840 |
| vaxis | 0 | 6,992 | 4,752 |
| ultraviolet direct (TerminalScreen) | 0 | 11,552 | 8,912 |
| awesome-gocui | 12.9 | 11,408 | 8,960 |

### Streaming: 10,000 events at 200/s (50 s)

| spike | runs | CPU ms (user+sys) | max RSS KB | RSS after done KB | bytes to pty | bytes/event | frames/View calls | producer wall ms | DONE on screen after last event ms | heap after GC KB |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| Bubble Tea v2 + bubbles viewport | 1 | 40,046 | 20,800 | 20,656 | 1,271,321 | 127 | 10,008 | 51,339 | 12 | 1,745 |
| Bubble Tea v2 + viewport, batched msgs | 1 | 40,303 | 20,848 | 20,688 | 1,261,612 | 126 | 9,751 | 49,996 | 22 | 1,758 |
| Bubble Tea v2 + windowed transcript | 1 | 9,347 | 20,544 | 20,368 | 1,261,616 | 126 | 10,008 | 49,996 | 5 | 1,763 |
| Bubble Tea v2 + windowed, batched msgs | 1 | 7,166 | 20,896 | 20,752 | 1,261,513 | 126 | 10,002 | 49,995 | 6 | 1,758 |
| Bubble Tea v2 + viewport, 16 ms batch window | 1 | 13,730 | 20,736 | 20,576 | 1,199,220 | 120 | 2,508 | 49,996 | 22 | 1,757 |
| Bubble Tea v2 + windowed, 16 ms batch window | 1 | 4,202 | 20,544 | 20,400 | 1,199,516 | 120 | 2,508 | 49,996 | 5 | 1,728 |
| tview (tcell v2) | 1 | 6,880 | 21,328 | 21,152 | 15,647,266 | 1,565 | 2,476 | 49,995 | 20 | 3,682 |
| tcell v3 raw | 1 | 4,010 | 14,240 | 14,016 | 13,244,515 | 1,324 | 2,946 | 49,995 | 17 | 1,680 |
| vaxis | 1 | 2,231 | 12,096 | 11,824 | 12,497,242 | 1,250 | 2,948 | 49,995 | 17 | 2,315 |
| ultraviolet direct (TerminalScreen) | 1 | 5,210 | 19,664 | 19,472 | 10,021,736 | 1,002 | 2,948 | 49,996 | 15 | 3,226 |
| ultraviolet direct (TerminalRenderer + scroll optim) | 1 | 3,972 | 18,144 | 17,952 | 971,806 | 97 | 2,949 | 49,996 | 15 | 2,668 |
| awesome-gocui | 1 | 6,640 | 63,296 | 63,072 | 13,620,890 | 1,362 | 2,502 | 49,995 | 9 | 24,981 |

### Burst: 10,000 events as fast as possible (median of 3)

| spike | runs | CPU ms (user+sys) | max RSS KB | RSS after done KB | bytes to pty | bytes/event | frames/View calls | producer wall ms | DONE on screen after last event ms | heap after GC KB |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| Bubble Tea v2 + bubbles viewport | 3 | 34,040 | 20,704 | 20,544 | 1,155,391 | 116 | 10,008 | 32,747 | 14 | 1,740 |
| Bubble Tea v2 + viewport, batched msgs | 3 | 139 | 18,768 | 18,304 | 19,996 | 2 | 20 | 71 | 30 | 1,694 |
| Bubble Tea v2 + windowed transcript | 3 | 1,215 | 21,024 | 20,848 | 176,215 | 18 | 10,008 | 924 | 15 | 1,723 |
| Bubble Tea v2 + windowed, batched msgs | 3 | 83 | 18,560 | 18,208 | 10,446 | 1 | 21 | 37 | 14 | 1,695 |
| Bubble Tea v2 + windowed, 16 ms batch window | 3 | 73 | 19,056 | 18,672 | 4,041 | 0 | 9 | 10 | 41 | 1,689 |
| tview (tcell v2) | 3 | 136 | 17,104 | 16,880 | 24,625 | 2 | 5 | 80 | 44 | 3,551 |
| tcell v3 raw | 3 | 35 | 12,160 | 11,488 | 12,380 | 1 | 4 | 10 | 8 | 1,637 |
| vaxis | 3 | 42 | 11,968 | 11,264 | 11,338 | 1 | 4 | 15 | 9 | 2,301 |
| ultraviolet direct (TerminalScreen) | 3 | 51 | 16,176 | 15,872 | 8,648 | 1 | 5 | 22 | 14 | 3,170 |
| ultraviolet direct (TerminalRenderer + scroll optim) | 3 | 48 | 15,760 | 15,424 | 6,258 | 1 | 5 | 26 | 9 | 2,633 |
| awesome-gocui | 3 | 96 | 51,584 | 51,280 | 12,944 | 1 | 4 | 12 | 52 | 24,939 |

### Burst, no frame coalescing (-fps 0: draw after every event)

| spike | runs | CPU ms (user+sys) | max RSS KB | RSS after done KB | bytes to pty | bytes/event | frames/View calls | producer wall ms | DONE on screen after last event ms | heap after GC KB |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| tview (tcell v2) | 1 | 10,386 | 19,952 | 19,776 | 63,112,756 | 6,311 | 10,002 | 9,618 | 0 | 3,562 |
| tcell v3 raw | 1 | 4,128 | 14,576 | 14,352 | 48,577,934 | 4,858 | 10,002 | 3,572 | 93 | 1,672 |
| vaxis | 1 | 1,627 | 12,528 | 12,240 | 45,700,297 | 4,570 | 10,002 | 1,548 | 169 | 2,308 |
| ultraviolet direct (TerminalScreen) | 1 | 4,301 | 19,600 | 19,408 | 33,931,450 | 3,393 | 10,002 | 3,696 | 1 | 3,200 |
| ultraviolet direct (TerminalRenderer + scroll optim) | 1 | 3,385 | 18,560 | 18,368 | 1,507,666 | 151 | 10,002 | 2,694 | 0 | 2,662 |
| awesome-gocui | 1 | 284 | 66,720 | 66,320 | 13,662 | 1 | 5 | 21 | 33 | 29,450 |

### Transcript growth: 50,000 events burst, all lines retained

| spike | runs | CPU ms (user+sys) | max RSS KB | RSS after done KB | bytes to pty | bytes/event | frames/View calls | producer wall ms | DONE on screen after last event ms | heap after GC KB |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| Bubble Tea v2 + bubbles viewport (TIMED OUT) | 1 | 245,231 | 25,008 | – | 4,277,743 | 86 | – | – | – | – |
| Bubble Tea v2 + viewport, batched msgs | 1 | 931 | 27,072 | 26,880 | 130,498 | 3 | 58 | 766 | 101 | 6,028 |
| Bubble Tea v2 + windowed transcript | 1 | 5,744 | 30,048 | 29,904 | 843,944 | 17 | 50,008 | 4,482 | 3 | 6,121 |
| Bubble Tea v2 + windowed, batched msgs | 1 | 185 | 26,928 | 26,656 | 26,263 | 1 | 58 | 124 | 10 | 6,101 |
| Bubble Tea v2 + windowed, 16 ms batch window | 1 | 198 | 31,584 | 31,344 | 10,530 | 0 | 11 | 61 | 73 | 5,979 |
| tview (tcell v2) | 1 | 477 | 36,688 | 36,480 | 62,755 | 1 | 11 | 384 | 38 | 15,677 |
| tcell v3 raw | 1 | 96 | 19,552 | 19,328 | 23,338 | 0 | 6 | 42 | 9 | 6,488 |
| vaxis | 1 | 111 | 20,320 | 20,096 | 25,064 | 1 | 7 | 54 | 15 | 7,157 |
| ultraviolet direct (TerminalScreen) | 1 | 121 | 24,896 | 24,384 | 19,120 | 0 | 8 | 69 | 16 | 8,026 |
| ultraviolet direct (TerminalRenderer + scroll optim) | 1 | 106 | 24,992 | 24,448 | 11,882 | 0 | 7 | 59 | 10 | 7,486 |
| awesome-gocui | 1 | 244 | 207,456 | 207,200 | 12,951 | 0 | 4 | 47 | 102 | 122,290 |

