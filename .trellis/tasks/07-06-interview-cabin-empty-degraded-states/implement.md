# 实施计划

1. RED：后端测试，atom-backed 且未索引时 `summary.state=retrieval_degraded`。
2. GREEN：补 `launchpad.summary.state`。
3. 前端改启动台空状态/降级态分支，不再把空轨道一律 fallback。
4. 浏览器 smoke 验证当前筛选无结果与降级文案。
