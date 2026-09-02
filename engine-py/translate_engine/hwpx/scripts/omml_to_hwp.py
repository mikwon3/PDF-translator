"""
OMML → HWP 수식 마크업 변환기

OMML (Office Math Markup Language, Microsoft) 수식을 한컴 글의 수식 편집기
명령어로 변환한다.

지원하는 변환:
- 위첨자/아래첨자 (sSub, sSup, sSubSup)
- 분수 (f)
- 제곱근 (rad)
- 괄호/구분자 (d)
- n-ary (int, sum, prod 등)
- 행렬 (m)
- 극한 (limLow, limUpp)
- 함수 (func)
- 그리스 문자 자동 인식
- 특수 연산자/기호 자동 인식
"""

# OMML 네임스페이스
M = '{http://schemas.openxmlformats.org/officeDocument/2006/math}'


# ════════════════════════════════════════════════════════════
# 유니코드 → 한컴 수식 명령어 매핑
# ════════════════════════════════════════════════════════════

# 그리스 문자
GREEK_MAP = {
    'α': 'alpha', 'β': 'beta', 'γ': 'gamma', 'δ': 'delta',
    'ε': 'epsilon', 'ζ': 'zeta', 'η': 'eta', 'θ': 'theta',
    'ι': 'iota', 'κ': 'kappa', 'λ': 'lambda', 'μ': 'mu',
    'ν': 'nu', 'ξ': 'xi', 'ο': 'omicron', 'π': 'pi',
    'ρ': 'rho', 'σ': 'sigma', 'τ': 'tau', 'υ': 'upsilon',
    'φ': 'phi', 'ϕ': 'phi', 'χ': 'chi', 'ψ': 'psi', 'ω': 'omega',
    # 대문자
    'Α': 'Alpha', 'Β': 'Beta', 'Γ': 'Gamma', 'Δ': 'Delta',
    'Ε': 'Epsilon', 'Ζ': 'Zeta', 'Η': 'Eta', 'Θ': 'Theta',
    'Ι': 'Iota', 'Κ': 'Kappa', 'Λ': 'Lambda', 'Μ': 'Mu',
    'Ν': 'Nu', 'Ξ': 'Xi', 'Ο': 'Omicron', 'Π': 'Pi',
    'Ρ': 'Rho', 'Σ': 'Sigma', 'Τ': 'Tau', 'Υ': 'Upsilon',
    'Φ': 'Phi', 'Χ': 'Chi', 'Ψ': 'Psi', 'Ω': 'Omega',
    # variants
    'ϑ': 'vartheta', 'ϖ': 'varpi', 'ς': 'varsigma',
    'ϵ': 'varepsilon', 'ϱ': 'varrho',
}

# 연산자/관계 기호
OP_MAP = {
    '×': 'times', '÷': 'div', '±': '+-', '∓': '-+',
    '·': 'cdot', '∙': 'cdot', '⋅': 'cdot',
    '≤': '<=', '≥': '>=', '≠': 'neq', '≈': 'approx', '≃': 'simeq',
    '≡': 'equiv', '∝': 'propto', '∼': 'sim',
    '∞': 'inf', '∂': 'partial', '∇': 'nabla',
    '∀': 'forall', '∃': 'exist', '∅': 'emptyset',
    '∈': 'in', '∉': 'notin', '∋': 'owns', '∌': 'notin',
    '⊂': 'subset', '⊃': 'supset', '⊆': 'subseteq', '⊇': 'supseteq',
    '∪': 'union', '∩': 'inter',
    # 화살표
    '→': 'rarrow', '←': 'larrow', '↑': 'uparrow', '↓': 'downarrow',
    '⇒': 'RARROW', '⇐': 'LARROW', '⇔': 'LRARROW',
    '↔': 'lrarrow', '↦': 'mapsto',
    # 적분/시그마/곱
    '∫': 'int', '∮': 'oint', '∑': 'sum', '∏': 'prod',
    '√': 'sqrt',
    # 기타
    '°': 'DEG', '∠': 'ANGLE', '⊥': 'BOT', '∥': 'VERT',
    '…': 'cdots', '⋯': 'cdots', '⋮': 'VDOTS', '⋱': 'DDOTS',
    # 다양한 공백
    '\u00A0': '~', '\u2009': '`', '\u202F': '`',
}

# n-ary 연산자 (적분/시그마 등)
NARY_MAP = {
    '∫': 'int', '∮': 'oint', '∬': 'dint', '∭': 'tint',
    '∯': 'odint', '∰': 'otint',
    '∑': 'sum', '∏': 'prod', '∐': 'coprod',
    '⋃': 'union', '⋂': 'inter', '⨄': 'uplus',
}

# 항상 로만체로 표현되는 함수 (한컴이 자동 처리)
FUNCTIONS = {
    'sin', 'cos', 'tan', 'cot', 'sec', 'csc', 'cosec',
    'sinh', 'cosh', 'tanh', 'coth',
    'arcsin', 'arccos', 'arctan',
    'log', 'ln', 'lg', 'exp', 'Exp',
    'max', 'min', 'lim', 'Lim', 'sup', 'inf',
    'det', 'gcd', 'mod', 'arg', 'deg', 'dim',
    'if', 'for', 'and', 'ker', 'hom', 'Pr',
}


def map_text(text):
    """텍스트를 HWP 수식 표현으로 변환 (그리스/특수문자 치환).
    그리스 문자/명령어 뒤에는 백틱(`)을 붙여 다음 글자와 구분.
    일반 공백은 한컴 수식의 빈칸 명령어(`~`)로 변환 — 한글 수식 편집기는
    일반 공백을 보존하지 않으므로 ~를 써야 한다.
    """
    if not text:
        return ''
    result = []
    for ch in text:
        if ch in GREEK_MAP:
            # 한컴 수식에서 명령어 뒤 백틱은 1/4 공백 - 명령어 경계 명확히
            result.append(GREEK_MAP[ch] + '`')
        elif ch in OP_MAP:
            result.append('`' + OP_MAP[ch] + '`')
        elif ch == ' ':
            # 일반 공백 → ~ (한컴 수식에서 보이는 빈칸)
            result.append('~')
        elif ch == '\t':
            result.append('~~')  # 탭은 두 칸 정도
        else:
            result.append(ch)
    return ''.join(result)


# ════════════════════════════════════════════════════════════
# OMML 트리 변환
# ════════════════════════════════════════════════════════════

def convert_text_run(elem):
    """<m:r> → 텍스트 + 그리스/연산자 치환. 앞뒤 공백 보존."""
    parts = []
    for t in elem.findall(f'{M}t'):
        if t.text:
            parts.append(map_text(t.text))
    return ''.join(parts)


def convert_e(elem):
    """<m:e> 등 컨테이너: 자식들을 순서대로 변환."""
    return ''.join(convert_node(c) for c in elem)


def convert_ssub(elem):
    """<m:sSub>: base와 sub로 분리해서 base_{sub}."""
    base = ''
    sub = ''
    for child in elem:
        tag = child.tag.split('}')[-1]
        if tag == 'e':
            base = convert_e(child)
        elif tag == 'sub':
            sub = convert_e(child)
    base_s = wrap_if_complex(base)
    return f'{base_s}_{{{sub.strip()}}}'


def convert_ssup(elem):
    """<m:sSup>: base와 sup."""
    base = ''
    sup = ''
    for child in elem:
        tag = child.tag.split('}')[-1]
        if tag == 'e':
            base = convert_e(child)
        elif tag == 'sup':
            sup = convert_e(child)
    base_s = wrap_if_complex(base)
    return f'{base_s}^{{{sup.strip()}}}'


def convert_ssubsup(elem):
    """<m:sSubSup>: base, sub, sup 모두."""
    base = ''
    sub = ''
    sup = ''
    for child in elem:
        tag = child.tag.split('}')[-1]
        if tag == 'e':
            base = convert_e(child)
        elif tag == 'sub':
            sub = convert_e(child)
        elif tag == 'sup':
            sup = convert_e(child)
    base_s = wrap_if_complex(base)
    return f'{base_s}_{{{sub.strip()}}}^{{{sup.strip()}}}'


def convert_frac(elem):
    """<m:f>: 분수 → {num} over {den}."""
    num = ''
    den = ''
    bar_type = 'bar'
    for child in elem:
        tag = child.tag.split('}')[-1]
        if tag == 'fPr':
            ft = child.find(f'{M}type')
            if ft is not None:
                bar_type = ft.get(f'{M}val', 'bar')
        elif tag == 'num':
            num = convert_e(child)
        elif tag == 'den':
            den = convert_e(child)
    op = 'atop' if bar_type in ('noBar', 'nobar') else 'over'
    return f'{{{num.strip()}}} {op} {{{den.strip()}}}'


def convert_rad(elem):
    """<m:rad>: 제곱근 (n제곱근 가능)."""
    deg = ''
    e = ''
    hide_deg = False
    for child in elem:
        tag = child.tag.split('}')[-1]
        if tag == 'radPr':
            dh = child.find(f'{M}degHide')
            if dh is not None:
                hide_deg = (dh.get(f'{M}val', 'false') in ('true', '1'))
        elif tag == 'deg':
            deg = convert_e(child)
        elif tag == 'e':
            e = convert_e(child)
    if hide_deg or not deg.strip():
        return f'sqrt {{{e.strip()}}}'
    return f'root {{{deg.strip()}}} of {{{e.strip()}}}'


def convert_delim(elem):
    """<m:d>: 괄호/구분자."""
    beg = '('
    end = ')'
    sep = '|'
    contents = []
    for child in elem:
        tag = child.tag.split('}')[-1]
        if tag == 'dPr':
            b = child.find(f'{M}begChr')
            e = child.find(f'{M}endChr')
            s = child.find(f'{M}sepChr')
            if b is not None:
                beg = b.get(f'{M}val', '(')
            if e is not None:
                end = e.get(f'{M}val', ')')
            if s is not None:
                sep = s.get(f'{M}val', '|')
        elif tag == 'e':
            contents.append(convert_e(child))

    # 빈 문자열은 . (없음 표시) 사용
    beg_str = beg if beg else '.'
    end_str = end if end else '.'

    # 한컴 수식에서 중괄호는 escape
    if beg_str == '{':
        beg_str = 'lbrace'
    elif beg_str == '}':
        beg_str = 'rbrace'
    if end_str == '{':
        end_str = 'lbrace'
    elif end_str == '}':
        end_str = 'rbrace'

    body = sep.join(contents)
    return f'left {beg_str} {body} right {end_str}'


def convert_nary(elem):
    """<m:nary>: 적분/시그마 등."""
    op_char = '∫'
    sub = ''
    sup = ''
    e = ''
    hide_sub = False
    hide_sup = False
    for child in elem:
        tag = child.tag.split('}')[-1]
        if tag == 'naryPr':
            chr_e = child.find(f'{M}chr')
            if chr_e is not None:
                op_char = chr_e.get(f'{M}val', '∫')
            sh = child.find(f'{M}subHide')
            if sh is not None and sh.get(f'{M}val', 'false') in ('true', '1'):
                hide_sub = True
            uh = child.find(f'{M}supHide')
            if uh is not None and uh.get(f'{M}val', 'false') in ('true', '1'):
                hide_sup = True
        elif tag == 'sub':
            sub = convert_e(child)
        elif tag == 'sup':
            sup = convert_e(child)
        elif tag == 'e':
            e = convert_e(child)

    op_name = NARY_MAP.get(op_char, 'int')
    result = op_name
    if sub.strip() and not hide_sub:
        result += f'_{{{sub.strip()}}}'
    if sup.strip() and not hide_sup:
        result += f'^{{{sup.strip()}}}'
    if e.strip():
        result += f' {{{e.strip()}}}'
    return result


def convert_func(elem):
    """<m:func>: 함수 (fName(arg))."""
    fname = ''
    e = ''
    for child in elem:
        tag = child.tag.split('}')[-1]
        if tag == 'fName':
            fname = convert_e(child)
        elif tag == 'e':
            e = convert_e(child)
    return f'{fname.strip()} {{{e.strip()}}}'


def convert_lim_low(elem):
    """<m:limLow>: lim의 아래 첨자 (lim_{x->0} 같은 형태)."""
    e = ''
    lim = ''
    for child in elem:
        tag = child.tag.split('}')[-1]
        if tag == 'e':
            e = convert_e(child)
        elif tag == 'lim':
            lim = convert_e(child)
    return f'{e.strip()}_{{{lim.strip()}}}'


def convert_lim_upp(elem):
    """<m:limUpp>: 위 첨자 형태."""
    e = ''
    lim = ''
    for child in elem:
        tag = child.tag.split('}')[-1]
        if tag == 'e':
            e = convert_e(child)
        elif tag == 'lim':
            lim = convert_e(child)
    return f'{e.strip()}^{{{lim.strip()}}}'


def convert_matrix(elem):
    """<m:m>: 행렬."""
    rows = []
    for child in elem:
        tag = child.tag.split('}')[-1]
        if tag == 'mr':
            cells = []
            for e in child.findall(f'{M}e'):
                cells.append(convert_e(e).strip())
            rows.append(' & '.join(cells))
    return 'matrix {' + ' # '.join(rows) + '}'


def convert_bar(elem):
    """<m:bar>: 위/아래 막대 (벡터, 평균 등)."""
    e = ''
    pos = 'top'
    for child in elem:
        tag = child.tag.split('}')[-1]
        if tag == 'barPr':
            p = child.find(f'{M}pos')
            if p is not None:
                pos = p.get(f'{M}val', 'top')
        elif tag == 'e':
            e = convert_e(child)
    if pos == 'bot':
        return f'under {{{e.strip()}}}'
    return f'bar {{{e.strip()}}}'


def convert_acc(elem):
    """<m:acc>: 악센트 (^, ~, → 등)."""
    e = ''
    chr_val = '^'
    for child in elem:
        tag = child.tag.split('}')[-1]
        if tag == 'accPr':
            c = child.find(f'{M}chr')
            if c is not None:
                chr_val = c.get(f'{M}val', '^')
        elif tag == 'e':
            e = convert_e(child)

    acc_map = {
        '^': 'hat', '~': 'tilde', '→': 'vec', '←': 'vec',
        '\u0303': 'tilde', '\u0302': 'hat', '\u0307': 'dot',
        '\u0308': 'ddot', '\u0304': 'bar', '\u030A': 'acute',
        '\u0300': 'grave', '\u20D7': 'vec', '\u20D6': 'vec',
    }
    name = acc_map.get(chr_val, 'hat')
    return f'{name} {{{e.strip()}}}'


def convert_box(elem):
    """<m:box>: 그룹화. 단순히 내용만."""
    for child in elem:
        tag = child.tag.split('}')[-1]
        if tag == 'e':
            return convert_e(child)
    return ''


def convert_borderbox(elem):
    """<m:borderBox>: 테두리 박스."""
    for child in elem:
        tag = child.tag.split('}')[-1]
        if tag == 'e':
            return convert_e(child)
    return ''


def convert_groupchr(elem):
    """<m:groupChr>: 그룹 문자 (괄호 같은 것)."""
    e = ''
    for child in elem:
        tag = child.tag.split('}')[-1]
        if tag == 'e':
            e = convert_e(child)
    return e


def convert_eqarr(elem):
    """
    OMML의 eqArr (equation array, 방정식 배열)을 HWP의 pile로 변환.

    OMML 구조:
        <m:eqArr>
          <m:eqArrPr>...</m:eqArrPr>  (선택)
          <m:e>...첫 번째 방정식...</m:e>
          <m:e>...두 번째 방정식...</m:e>
          ...
        </m:eqArr>

    변환 결과: pile{eq1 # eq2 # ...}
    - pile: 가운데 정렬 (기본)
    - '#'은 HWP 수식의 행 구분자
    - 각 행이 복잡하면 중괄호로 감싸 명확히 분리
    """
    rows = []
    for child in elem:
        tag = child.tag.split('}')[-1]
        if tag == 'e':
            row = convert_e(child).strip()
            if row:
                rows.append(row)
        # eqArrPr, ctrlPr 등 속성 노드는 무시

    if not rows:
        return ''
    if len(rows) == 1:
        return rows[0]

    # 각 행을 wrap - 복잡하면 중괄호
    wrapped = [wrap_if_complex(r) for r in rows]
    body = ' # '.join(wrapped)
    return 'pile{' + body + '}'


def convert_phant(elem):
    """<m:phant>: phantom (보이지 않는 공간)."""
    return ''


# 노드 타입별 변환 디스패치
NODE_HANDLERS = {
    'r': convert_text_run,
    'sSub': convert_ssub,
    'sSup': convert_ssup,
    'sSubSup': convert_ssubsup,
    'f': convert_frac,
    'rad': convert_rad,
    'd': convert_delim,
    'nary': convert_nary,
    'func': convert_func,
    'limLow': convert_lim_low,
    'limUpp': convert_lim_upp,
    'm': convert_matrix,
    'eqArr': convert_eqarr,
    'bar': convert_bar,
    'acc': convert_acc,
    'box': convert_box,
    'borderBox': convert_borderbox,
    'groupChr': convert_groupchr,
    'phant': convert_phant,
    # 컨테이너
    'oMath': lambda e: convert_e(e),
    'oMathPara': lambda e: convert_e(e),
    'e': convert_e,
    # 제어 노드는 출력 X
    'sSubPr': lambda e: '',
    'sSupPr': lambda e: '',
    'sSubSupPr': lambda e: '',
    'fPr': lambda e: '',
    'radPr': lambda e: '',
    'dPr': lambda e: '',
    'naryPr': lambda e: '',
    'mPr': lambda e: '',
    'eqArrPr': lambda e: '',
    'barPr': lambda e: '',
    'accPr': lambda e: '',
    'ctrlPr': lambda e: '',
    'argPr': lambda e: '',
    'rPr': lambda e: '',
}


def convert_node(elem):
    """OMML 노드를 HWP 수식 마크업으로 변환."""
    tag = elem.tag.split('}')[-1]
    handler = NODE_HANDLERS.get(tag)
    if handler is None:
        # 알 수 없는 노드 - 자식들을 재귀
        return ''.join(convert_node(c) for c in elem)
    return handler(elem)


def wrap_if_complex(s):
    """문자열이 복잡하면(공백/명령어 포함) 중괄호로 감싼다."""
    s = s.strip()
    if not s:
        return '{}'
    if len(s) == 1:
        return s
    # 한 단어이고 명령어가 아니면 그대로
    if ' ' not in s and '{' not in s and '_' not in s and '^' not in s:
        return s
    if s.startswith('{') and s.endswith('}'):
        return s
    return '{' + s + '}'


def omml_to_hwp_equation(omml_elem):
    """
    OMML 요소(oMath 또는 oMathPara)를 HWP 수식 마크업 문자열로 변환.
    """
    result = convert_node(omml_elem)
    import re
    # 1) 연속 백틱 정리 (`````` → `, `` → 공백 또는 제거)
    # 백틱은 1/4 공백 명령이므로 2개 이상 연속이면 의미 없음 - 1개로 축소
    result = re.sub(r'`{2,}', '`', result)
    # 2) 백틱이 명령어 사이에 잘못 끼어든 경우 (예: "alpha``beta")는 공백으로
    result = re.sub(r'([a-zA-Z]+)`+([a-zA-Z]+)', r'\1 \2', result)
    # 3) 연속 일반 공백만 정리 (~ 는 보존)
    result = re.sub(r'  +', ' ', result)
    # 4) 백틱 주위 정리
    result = re.sub(r'` +', '` ', result)
    result = re.sub(r' +`', ' `', result)
    # 5) 연속 ~ 는 그대로 유지 (의도된 공백)
    # 6) 앞뒤 일반 공백/백틱 제거 (수식 양끝)
    return result.strip(' `')


# 한컴 수식의 "구조적" 명령어 - 이게 들어가야 진짜 수식 객체로 만들 가치가 있음
# (단순 변수명, 첨자만 있으면 일반 텍스트로 표시해도 한컴이 처리 가능)
_HWP_STRUCT_CMDS = {
    'over', 'atop',           # 분수
    'sqrt', 'root',           # 제곱근
    'matrix', 'pmatrix', 'bmatrix', 'dmatrix',  # 행렬
    'cases', 'pile', 'lpile', 'rpile',          # 묶음/쌓기
    'sum', 'int', 'oint', 'prod', 'coprod',     # 적분/시그마
    'union', 'inter', 'lim',                    # 극한
    'binom', 'choose',                          # 조합
    'left', 'right',                            # 자동 크기 괄호
    'bigg', 'big',                              # 큰 기호
    'hat', 'tilde', 'bar', 'vec', 'acute', 'grave', 'dot', 'ddot',  # 악센트
    'under',
    'rm', 'bold', 'it',                         # 글꼴 변환
}


def needs_equation_object(hwp_script):
    """
    OMML 수식은 모두 hp:equation 객체로 변환한다.

    배경: v9에서는 crash를 우려해 단순 첨자만 있는 수식(t_5, f_1 등)은 텍스트로
    처리했다. 그러나 v13에서 진짜 crash 원인이 raw tab 문자(U+0009)를 hp:t에
    직접 넣은 것이었음이 밝혀져 수정되었고, 그 이후 수식 객체 개수와 crash 간
    직접적 인과관계가 없음이 확인되었다.

    원본 docx에서 OMML로 표기된 것은 저자가 명시적으로 "수식"으로 만든 것이므로
    한글에서도 수식 객체(hp:equation)로 표현되어 첨자·기울임 등이 자연스럽게
    보이는 것이 옳다. 예: t_5 → t 아래에 작게 5.

    Returns:
        True - hp:equation 객체로 (첨자·이탤릭·정렬 자동 처리됨)
        False - 스크립트가 비어있는 경우만
    """
    if not hwp_script or not hwp_script.strip():
        return False
    return True


def _needs_equation_object_legacy(hwp_script):
    """참고용 이전 판단 로직 (v9~v14). 지금은 사용 안 함."""
    import re
    if not hwp_script or not hwp_script.strip():
        return False
    script_lower = hwp_script.lower()
    for cmd in _HWP_STRUCT_CMDS:
        if re.search(r'\b' + cmd + r'\b', script_lower):
            return True
    depth = 0
    max_depth = 0
    for ch in hwp_script:
        if ch == '{':
            depth += 1
            max_depth = max(max_depth, depth)
        elif ch == '}':
            depth -= 1
    if max_depth >= 2:
        return True
    return False


def script_to_plain_text(hwp_script):
    """
    수식 객체가 아닌 일반 텍스트로 변환할 때 사용할 단순화된 표현.

    예:
    - 'EI' → 'EI'
    - 't_5' → 't_5' (그대로 유지 - 가독성 위해)
    - 'f_{ck}' → 'f_ck' (괄호 제거)
    - 'lambda' → 'λ' (그리스 문자 명령어는 유니코드로 되돌림)
    """
    import re
    text = hwp_script

    # 그리스 문자 명령어 → 유니코드 (GREEK_MAP의 역매핑)
    greek_reverse = {v: k for k, v in GREEK_MAP.items()}
    # 긴 이름부터 매칭 (alpha가 a보다 먼저)
    sorted_names = sorted(greek_reverse.keys(), key=len, reverse=True)
    for name in sorted_names:
        text = re.sub(r'\b' + name + r'\b', greek_reverse[name], text)

    # 연산자 명령어 → 기호 (OP_MAP의 역매핑) - 일부만
    op_reverse = {
        'times': '×', 'div': '÷', 'cdot': '·',
        '<=': '≤', '>=': '≥', 'neq': '≠', '!=': '≠',
        'approx': '≈', 'equiv': '≡',
        '+-': '±', '-+': '∓',
        'inf': '∞', 'partial': '∂',
        'rarrow': '→', 'larrow': '←',
        '->': '→', '<-': '←',
    }
    for cmd, sym in sorted(op_reverse.items(), key=lambda x: -len(x[0])):
        text = re.sub(r'\b' + re.escape(cmd) + r'\b', sym, text)

    # 백틱 (수식 빈칸) 제거
    text = text.replace('`', '')
    # ~ (수식 공백) → 일반 공백
    text = text.replace('~', ' ')
    # 중괄호 제거 (단, 의미 보존 시도)
    # f_{ck} → f_ck
    text = re.sub(r'\{([^{}]*)\}', r'\1', text)

    # 연속 공백 정리
    text = re.sub(r'  +', ' ', text)
    return text.strip()
