
import html, re, sys, os

def esc(s):
    return html.escape(s, quote=True)

def main(src, dst):
    lines = open(src, encoding="utf-8").read().split("\n")
    # Drop a trailing blank run so the frame is not mostly empty.
    while lines and lines[-1] == "":
        lines.pop()
    if not lines:
        print("empty session", file=sys.stderr)
        return 1

    FS = 13.5          # font size
    CW = FS * 0.6015   # monospace advance width
    LH = 21.0          # line height
    PAD = 18.0
    CHROME = 30.0      # window title bar

    cols = max(len(l) for l in lines)
    width = int(cols * CW + PAD * 2)
    height = int(len(lines) * LH + PAD * 2 + CHROME)

    # Reading pace. The first version ran at 0.085s per line, which is a terminal
    # at full speed: the transcript was over before anyone could read it. These
    # values let a reader follow, and the environment can override both.
    STEP = float(os.environ.get("STEP", "0.36"))    # seconds between lines
    HOLD = float(os.environ.get("HOLD", "15.0"))    # seconds the full text stays
    cycle = len(lines) * STEP + HOLD

    def colour(l):
        if l.startswith("#"):
            return "#6e7681"          # narration
        if l.startswith("$"):
            return "#79c0ff"          # command
        if re.match(r"^\s*-", l):
            return "#8b949e"          # findings
        if l.startswith("  "):
            return "#a5d6ff"          # report body
        return "#c9d1d9"              # output

    css = []
    css.append("@keyframes blink { 0%,49% {opacity:1} 50%,100% {opacity:0} }")
    for i in range(len(lines)):
        a = max(0.0, (i * STEP) / cycle * 100 - 0.35)
        b = a + 0.35
        css.append(
            "@keyframes k%d { 0%%,%.2f%% { opacity:0 } %.2f%% { opacity:1 } 96%% { opacity:1 } 100%% { opacity:0 } }"
            % (i, a, b)
        )

    out = []
    out.append(
        '<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d" '
        'font-family="ui-monospace,SFMono-Regular,Menlo,Consolas,&quot;Liberation Mono&quot;,monospace">'
        % (width, height, width, height)
    )
    out.append("<style>")
    out.append("".join(css))
    out.append(".l { opacity: 0; }")
    out.append("</style>")

    # window
    out.append('<rect width="%d" height="%d" rx="10" fill="#0d1117"/>' % (width, height))
    out.append('<rect width="%d" height="%d" rx="10" fill="none" stroke="#30363d"/>' % (width, height))
    out.append('<rect x="0" y="0" width="%d" height="%d" rx="10" fill="#161b22"/>' % (width, CHROME))
    out.append('<rect x="0" y="%d" width="%d" height="%d" fill="#161b22"/>' % (CHROME - 10, width, 10))
    for idx, c in enumerate(["#ff5f56", "#ffbd2e", "#27c93f"]):
        out.append('<circle cx="%d" cy="%d" r="5.5" fill="%s"/>' % (PAD + 8 + idx * 18, CHROME / 2, c))
    out.append(
        '<text x="%d" y="%d" fill="#8b949e" font-size="11.5">crawler@edge: ~</text>'
        % (width - PAD - 12 * 11.5 * 0.6015, CHROME / 2 + 4)
    )

    y = CHROME + PAD + FS
    for i, l in enumerate(lines):
        if l.strip() == "":
            y += LH
            continue
        out.append(
            '<text class="l" style="animation:k%d %.2fs infinite" x="%.1f" y="%.1f" fill="%s" '
            'font-size="%.1f" xml:space="preserve">%s</text>'
            % (i, cycle, PAD, y, colour(l), FS, esc(l))
        )
        y += LH

    # cursor, which idles once the transcript has finished
    out.append(
        '<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" fill="#c9d1d9">'
        '<animate attributeName="opacity" values="1;0;1" dur="1.05s" repeatCount="indefinite"/>'
        "</rect>" % (PAD, y - LH + 3, CW, FS + 1)
    )

    out.append("</svg>")
    open(dst, "w", encoding="utf-8").write("\n".join(out) + "\n")
    print("wrote %s: %d lines, %dx%d, cycle %.1fs" % (dst, len(lines), width, height, cycle))
    return 0

if __name__ == "__main__":
    sys.exit(main(sys.argv[1], sys.argv[2]))
