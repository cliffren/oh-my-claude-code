# omcc Handoff

## 已完成
- 修复核心 bug：文本输出、thinking 显示、模型切换、slash 命令透传
- 鼠标支持：滚轮滚动 + 点击自动切换到终端选中模式
- Effort 切换（Ctrl+X）：low/medium/high/max，传递 --effort 给 claude CLI
- "/" 命令面板：内部命令（/model /sessions /theme /effort /new /help）+ Claude CLI 命令
- Ctrl+C 中断 agent，双击退出；表格渲染宽度修复；markdown renderer 缓存

## 已知问题
- thinking 块渲染样式仍可优化（长文本截断为 20 行）
- 鼠标自动切换在部分终端可能行为不一致（依赖 tea.MouseActionPress 检测）
- slash 命令列表为硬编码，未从 claude CLI init 事件动态获取

## 下一步
- 从 claude CLI 的 init 事件解析 `slash_commands` 字段，动态更新命令列表
- 支持 Claude Code 的 plan mode 切换
- 优化 thinking 块：可折叠/展开，而非固定截断
