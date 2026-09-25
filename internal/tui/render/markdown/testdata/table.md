| Case | Before | After | Note |
| --- | ---: | :---: | --- |
| cold render | 1.2 ms | **0.9 ms** | parse and draw everything once |
| warm | 400 µs | 2 µs | `cached` |
| 表格 wide | 😀 | ok | emoji and CJK take two cells each |
