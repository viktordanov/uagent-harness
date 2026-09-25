```diff
@@ -210,3 +210,4 @@ func (m *Model) resume
-    run := m.store.MustLoad(id)
+    m.waitIdle()
+    run, err := m.store.Load(id)
     m.run = run
```

```go
func (m *Model) resume(id string) tea.Cmd {
	return m.listen(run, 64)
}
```

```a-very-long-language-name
short
```
