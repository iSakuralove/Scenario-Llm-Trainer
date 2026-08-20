"""从 LLM 的 JSON token 流中增量提取顶层字符串字段的未转义文本。"""

from __future__ import annotations


class StreamingFieldExtractor:
    """扫描拼接中的 JSON 文本，增量产出 ``field`` 字段字符串值内的未转义片段。

    只处理"顶层、字符串值"的常见形态（本项目 MentorAction 的 reply 字段）。
    不做完整 JSON 解析——流中途的残缺 JSON 无法 json.loads，而字段级状态机
    可以在值尚未闭合时就安全吐出已完成的转义片段。

    顶层之外（数组、嵌套对象）的内容用括号深度 + 字符串感知整体跳过，
    避免数组元素 "e1" 被误认为键、"] " 之后的引号错位等失同步问题。
    键名带引号精确比较，避免 "xreply" 误命中 "reply"。
    转义序列（含 \\uXXXX）可能跨 chunk 断开，统一用 _pending_escape 积累。
    """

    _SEEK_KEY = 0
    _IN_KEY = 1
    _SEEK_COLON = 2
    _SEEK_VALUE = 3
    _IN_STRING = 4
    _SKIP_VALUE = 5
    _DONE = 6
    _SEEK_COLON_ANY = 7
    _SEEK_VALUE_ANY = 8

    def __init__(self, field: str) -> None:
        self._target_key = f'"{field}"'
        self._state = self._SEEK_KEY
        self._key_buffer: list[str] = []
        self._pending_escape: list[str] = []
        self._skip_depth = 0  # _SKIP_VALUE 中的括号深度（[ 与 { 各算一层）
        self._skip_in_string = False

    def feed(self, delta: str) -> str:
        """喂入一段新增文本，返回本次可安全显示的未转义增量（可为空串）。"""
        out: list[str] = []
        for ch in delta:
            if self._state == self._DONE:
                break
            if self._state == self._IN_STRING:
                self._step_in_string(ch, out)
                continue
            if self._state == self._SKIP_VALUE:
                self._step_skip(ch)
                continue
            if ch in " \t\r\n":
                continue
            if self._state == self._SEEK_KEY:
                if ch == '"':
                    self._state = self._IN_KEY
                    self._key_buffer = []
                # '{'、'}'、','、':' 等一律忽略，等下一个引号开键。
                continue
            if self._state == self._IN_KEY:
                if ch == '"':
                    key = '"' + "".join(self._key_buffer) + '"'
                    self._state = self._SEEK_COLON if key == self._target_key else self._SEEK_COLON_ANY
                else:
                    self._key_buffer.append(ch)
                continue
            if self._state == self._SEEK_COLON:
                if ch == ":":
                    self._state = self._SEEK_VALUE
                continue
            if self._state == self._SEEK_COLON_ANY:
                if ch == ":":
                    self._state = self._SEEK_VALUE_ANY
                elif ch == '"':
                    # 上一段其实是数组元素等形态：当作新键处理。
                    self._state = self._IN_KEY
                    self._key_buffer = []
                continue
            if self._state == self._SEEK_VALUE_ANY:
                # 非目标键的值：字符串/数字/数组/对象都整体跳过。
                self._state = self._SKIP_VALUE
                self._skip_depth = 0
                self._skip_in_string = False
                self._step_skip(ch)
                continue
            # _SEEK_VALUE（目标键之后）
            if ch == '"':
                self._state = self._IN_STRING
                self._pending_escape = []
            else:
                # 数字/bool/null/数组/对象：目标字段形态不符或异常，跳过后终止。
                self._state = self._SKIP_VALUE
                self._skip_depth = 0
                self._skip_in_string = False
                self._step_skip(ch)
            continue
        return "".join(out)

    def _step_skip(self, ch: str) -> None:
        """跳过任意值（字符串/数字/数组/对象）：括号配平 + 字符串感知。"""
        if self._skip_in_string:
            if self._pending_escape:
                self._pending_escape = []
            elif ch == "\\":
                self._pending_escape = ["\\"]
            elif ch == '"':
                self._skip_in_string = False
                self._pending_escape = []
                if self._skip_depth == 0:
                    # 标量字符串值整体结束。
                    self._state = self._SEEK_KEY
            return
        if self._pending_escape:
            self._pending_escape = []
            return
        if ch == '"':
            # 字符串值开始（depth==0）或容器内字符串元素（depth>0）。
            self._skip_in_string = True
            self._pending_escape = []
            return
        if ch in "[{":
            self._skip_depth += 1
            return
        if ch in "]}":
            self._skip_depth -= 1
            if self._skip_depth <= 0:
                self._state = self._SEEK_KEY
            return
        if ch == "," and self._skip_depth == 0:
            # 标量（数字/bool/null）值在对象顶层由逗号分隔：值到此结束。
            self._state = self._SEEK_KEY
            return
        # 其他字符（数字/字母/点/负号等）：继续标量积累。

    def _step_in_string(self, ch: str, out: list[str]) -> None:
        if self._pending_escape:
            head = self._pending_escape[0]
            if head == "u" and len(self._pending_escape) < 5:
                if ch in "0123456789abcdefABCDEF":
                    self._pending_escape.append(ch)
                    if len(self._pending_escape) == 5:
                        out.append(self._decode_pending_unicode())
                        self._pending_escape = []
                    return
                # 非法 \u 序列：原样吐出，当前 ch 落入普通字符处理。
                out.append(self._escape_repr("".join(self._pending_escape)))
                self._pending_escape = []
            elif head == "\\":
                if ch == "u":
                    self._pending_escape = ["u"]
                    return
                if ch == '"':
                    out.append('"')
                    self._pending_escape = []
                    return
                out.append(self._decode_simple_escape(ch))
                self._pending_escape = []
                return
            else:  # pragma: no cover - 状态不会到达
                self._pending_escape = []
        if ch == '"':
            self._state = self._DONE
            return
        if ch == "\\":
            self._pending_escape = ["\\"]
            return
        out.append(ch)

    def _decode_pending_unicode(self) -> str:
        try:
            return chr(int("".join(self._pending_escape[1:5]), 16))
        except ValueError:  # pragma: no cover - 已校验十六进制
            return self._escape_repr("".join(self._pending_escape))

    @staticmethod
    def _decode_simple_escape(ch: str) -> str:
        mapping = {"n": "\n", "t": "\t", "r": "\r", "b": "\b", "f": "\f", '"': '"', "\\": "\\", "/": "/"}
        return mapping.get(ch, "\\" + ch)

    @staticmethod
    def _escape_repr(raw: str) -> str:
        return "\\" + raw
