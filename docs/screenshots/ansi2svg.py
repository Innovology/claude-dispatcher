#!/usr/bin/env python3
"""Turn a captured terminal screen into an SVG for the README.

Reads `tmux capture-pane -pe` output on stdin (text plus SGR escapes) and emits
a self-contained SVG: one <text> per row, one <tspan> per colour run, plus
<rect>s for background runs. No embedded font — the old screenshots carried a
378KB base64 webfont each; this references the same monospace stack the design
uses and stays around 40KB.

Usage: ansi2svg.py TITLE < capture.ansi > out.svg
"""
import re, sys, html

def _palette():
    """Read the page colours from palette.go, so a re-theme can never leave the
    screenshots a shade behind the product. Falls back only if the constants
    move or are renamed."""
    import os, re
    src = os.path.join(os.path.dirname(__file__), "..", "..",
                       "internal", "cockpit", "palette.go")
    fg, bg = "#e3e8ee", "#0f1319"
    try:
        text = open(src, encoding="utf-8").read()
        if m := re.search(r'cFg\s*=\s*"(#[0-9a-fA-F]{6})"', text):
            fg = m.group(1)
        if m := re.search(r'cSurface\s*=\s*"(#[0-9a-fA-F]{6})"', text):
            bg = m.group(1)
    except OSError:
        pass
    return fg, bg

FG, BG = _palette()
FONT = "ui-monospace, SFMono-Regular, Menlo, Consolas, 'DejaVu Sans Mono', monospace"
SIZE, LH, ADV = 13.0, 20.0, 7.82   # px: font size, line height, character advance
PADX, PADY = 16.0, 14.0

# xterm-256 palette: 16 system colours, a 6x6x6 cube, then the grey ramp.
def _p256():
    base = [(0,0,0),(205,0,0),(0,205,0),(205,205,0),(0,0,238),(205,0,205),(0,205,205),(229,229,229),
            (127,127,127),(255,0,0),(0,255,0),(255,255,0),(92,92,255),(255,0,255),(0,255,255),(255,255,255)]
    out = ["#%02x%02x%02x" % c for c in base]
    lv = [0, 95, 135, 175, 215, 255]
    for r in lv:
        for g in lv:
            for b in lv:
                out.append("#%02x%02x%02x" % (r, g, b))
    for i in range(24):
        v = 8 + i * 10
        out.append("#%02x%02x%02x" % (v, v, v))
    return out
P256 = _p256()

SGR = re.compile(r"\x1b\[([0-9;]*)m")
OSC = re.compile(r"\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)")
CSI = re.compile(r"\x1b\[[0-9;?]*[A-Za-z]")

def parse(line):
    """-> [(text, fg, bg, bold)] runs for one row."""
    runs, fg, bg, bold, i = [], None, None, False, 0
    for m in SGR.finditer(line):
        if m.start() > i:
            runs.append((line[i:m.start()], fg, bg, bold))
        codes = [int(x) if x else 0 for x in (m.group(1) or "0").split(";")]
        j = 0
        while j < len(codes):
            c = codes[j]
            if c == 0:
                fg, bg, bold = None, None, False
            elif c == 1:
                bold = True
            elif c == 22:
                bold = False
            elif c == 39:
                fg = None
            elif c == 49:
                bg = None
            elif 30 <= c <= 37:
                fg = P256[c - 30]
            elif 40 <= c <= 47:
                bg = P256[c - 40]
            elif 90 <= c <= 97:
                fg = P256[c - 90 + 8]
            elif 100 <= c <= 107:
                bg = P256[c - 100 + 8]
            elif c in (38, 48):
                tgt = "fg" if c == 38 else "bg"
                if j + 1 < len(codes) and codes[j + 1] == 5:
                    col = P256[codes[j + 2] % 256] if j + 2 < len(codes) else None
                    j += 2
                elif j + 1 < len(codes) and codes[j + 1] == 2:
                    r, g, b = (codes[j + 2: j + 5] + [0, 0, 0])[:3]
                    col = "#%02x%02x%02x" % (r & 255, g & 255, b & 255)
                    j += 4
                else:
                    col = None
                if tgt == "fg":
                    fg = col
                else:
                    bg = col
            j += 1
        i = m.end()
    if i < len(line):
        runs.append((line[i:], fg, bg, bold))
    return runs

def main():
    title = sys.argv[1] if len(sys.argv) > 1 else "claude-dispatcher"
    raw = sys.stdin.read().replace("\r", "")
    raw = OSC.sub("", raw)
    lines = [CSI.sub("", ln) if "\x1b[" in ln and not SGR.search(ln) else ln
             for ln in raw.split("\n")]
    while lines and not lines[-1].strip():
        lines.pop()

    rows = [parse(ln) for ln in lines]
    cols = max((sum(len(t) for t, *_ in r) for r in rows), default=80)
    W = cols * ADV + 2 * PADX
    H = len(rows) * LH + 2 * PADY

    out = [f'<svg xmlns="http://www.w3.org/2000/svg" width="{W:.0f}" height="{H:.0f}" '
           f'viewBox="0 0 {W:.0f} {H:.0f}" role="img" aria-label="{html.escape(title)}">',
           f'<title>{html.escape(title)}</title>',
           f'<rect width="{W:.0f}" height="{H:.0f}" rx="6" fill="{BG}"/>',
           f'<g font-family="{FONT}" font-size="{SIZE}" xml:space="preserve">']

    # Background runs first so text always paints over them.
    for r, runs in enumerate(rows):
        x = 0
        for text, _fg, bg, _b in runs:
            # A selected row's highlight covers its padding too, so a run is
            # painted whenever it has a background — blank or not.
            if bg:
                out.append(f'<rect x="{PADX + x*ADV:.2f}" y="{PADY + r*LH:.2f}" '
                           f'width="{len(text)*ADV:.2f}" height="{LH:.2f}" fill="{bg}"/>')
            x += len(text)

    for r, runs in enumerate(rows):
        y = PADY + r * LH + SIZE
        spans, x = [], 0
        for text, fg, _bg, bold in runs:
            if text:
                attrs = f' x="{PADX + x*ADV:.2f}"'
                if fg and fg != FG:
                    attrs += f' fill="{fg}"'
                if bold:
                    attrs += ' font-weight="600"'
                spans.append(f'<tspan{attrs}>{html.escape(text)}</tspan>')
                x += len(text)
        if spans:
            out.append(f'<text xml:space="preserve" y="{y:.2f}" fill="{FG}">'
                       + "".join(spans) + "</text>")

    out.append("</g></svg>")
    sys.stdout.write("\n".join(out) + "\n")

if __name__ == "__main__":
    main()
