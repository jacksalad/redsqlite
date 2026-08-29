/**
 * 轻量 SQL 格式化（V1.2）：关键字大写、主要子句换行、JOIN 分行、
 * 归一空白。字符串字面量、引号标识符与注释原样保留。
 * 不是完整解析器，目标是可读的单层缩进风格。
 */

const KEYWORDS = new Set([
  "SELECT", "FROM", "WHERE", "GROUP", "BY", "HAVING", "ORDER", "LIMIT", "OFFSET",
  "AS", "ON", "USING", "JOIN", "LEFT", "RIGHT", "FULL", "INNER", "OUTER", "CROSS", "NATURAL",
  "AND", "OR", "NOT", "NULL", "IS", "IN", "LIKE", "GLOB", "BETWEEN", "EXISTS", "CASE", "WHEN",
  "THEN", "ELSE", "END", "DISTINCT", "ALL", "UNION", "INTERSECT", "EXCEPT", "VALUES",
  "INSERT", "INTO", "UPDATE", "SET", "DELETE", "CREATE", "TABLE", "VIEW", "INDEX", "TRIGGER",
  "DROP", "ALTER", "ADD", "COLUMN", "PRIMARY", "KEY", "FOREIGN", "REFERENCES", "UNIQUE",
  "CHECK", "DEFAULT", "COLLATE", "AUTOINCREMENT", "WITH", "RECURSIVE", "RETURNING",
  "ASC", "DESC", "CAST", "PRAGMA", "EXPLAIN", "QUERY", "PLAN", "VACUUM", "ANALYZE",
  "BEGIN", "COMMIT", "ROLLBACK", "SAVEPOINT", "RELEASE", "ATTACH", "DETACH", "REPLACE",
  "CONFLICT", "DO", "NOTHING", "STRICT", "WITHOUT", "ROWID", "IF", "OVER", "PARTITION",
]);

/** 触发换行的子句关键字（GROUP BY / ORDER BY 只在 GROUP / ORDER 前换行）。 */
const CLAUSE_BREAK = new Set([
  "FROM", "WHERE", "GROUP", "HAVING", "ORDER", "LIMIT", "OFFSET", "WINDOW",
  "UNION", "INTERSECT", "EXCEPT", "VALUES", "RETURNING", "SET", "JOIN",
]);

/** JOIN 修饰词：仅当后随 JOIN 时换行（LEFT JOIN 整体一行）。 */
const JOIN_MODIFIERS = new Set(["LEFT", "RIGHT", "FULL", "INNER", "OUTER", "CROSS", "NATURAL"]);

/** 二元运算符（前后留空格）。 */
const OPERATOR_CHARS = new Set(["=", "<", ">", "+", "%", "|", "&", "^", "!"]);

type TokenType = "word" | "string" | "comment" | "punct";
interface Token {
  type: TokenType;
  text: string;
  upper: string; // word 的大写形式，其余同 text
}

function tokenize(sql: string): Token[] {
  const tokens: Token[] = [];
  let i = 0;
  const n = sql.length;
  while (i < n) {
    const c = sql[i];
    if (/\s/.test(c)) {
      i++;
      continue;
    }
    if (c === "-" && sql[i + 1] === "-") {
      const end = sql.indexOf("\n", i);
      const stop = end === -1 ? n : end;
      tokens.push({ type: "comment", text: sql.slice(i, stop), upper: "" });
      i = stop;
      continue;
    }
    if (c === "/" && sql[i + 1] === "*") {
      const end = sql.indexOf("*/", i + 2);
      const stop = end === -1 ? n : end + 2;
      tokens.push({ type: "comment", text: sql.slice(i, stop), upper: "" });
      i = stop;
      continue;
    }
    if (c === "'" || c === '"' || c === "`") {
      let j = i + 1;
      while (j < n) {
        if (sql[j] === c) {
          if (sql[j + 1] === c) {
            j += 2; // 转义的引号（'' / ""）
            continue;
          }
          j++;
          break;
        }
        j++;
      }
      tokens.push({ type: "string", text: sql.slice(i, j), upper: "" });
      i = j;
      continue;
    }
    if (c === "[") {
      const end = sql.indexOf("]", i + 1);
      const stop = end === -1 ? n : end + 1;
      tokens.push({ type: "string", text: sql.slice(i, stop), upper: "" });
      i = stop;
      continue;
    }
    if (/[A-Za-z0-9_$]/.test(c)) {
      let j = i;
      while (j < n && /[A-Za-z0-9_$]/.test(sql[j])) j++;
      const word = sql.slice(i, j);
      tokens.push({ type: "word", text: word, upper: word.toUpperCase() });
      i = j;
      continue;
    }
    tokens.push({ type: "punct", text: c, upper: "" });
    i++;
  }
  return tokens;
}

const INDENT = "  ";

/** 格式化 SQL 文本；空输入返回空串。 */
export function formatSql(sql: string): string {
  const tokens = tokenize(sql);
  const out: string[] = [];
  let depth = 0;
  let pendingNewline = false; // 下一个 token 前需要换行 + 缩进

  const appendBreak = () => {
    out.push("\n" + INDENT.repeat(depth));
  };

  const pushSpace = () => {
    const last = out[out.length - 1];
    if (!last) return;
    const lastChar = last[last.length - 1];
    if (lastChar !== "(" && lastChar !== ".") out.push(" ");
  };

  for (let i = 0; i < tokens.length; i++) {
    const t = tokens[i];
    const prev = tokens[i - 1];
    const next = tokens[i + 1];

    if (t.type === "comment") {
      if (out.length > 0) out.push("\n" + INDENT.repeat(depth));
      out.push(t.text);
      pendingNewline = true;
      continue;
    }

    if (t.type === "punct") {
      if (t.text === "(") {
        // 函数名后不留空格；关键字（AND ( / IN ( / VALUES (）后留空格
        const prevIsWordKeyword = prev?.type === "word" && KEYWORDS.has(prev.upper);
        if (prevIsWordKeyword) pushSpace();
        out.push("(");
        depth++;
        pendingNewline = false;
        continue;
      }
      if (t.text === ")") {
        depth = Math.max(0, depth - 1);
        out.push(")");
        pendingNewline = false;
        continue;
      }
      if (t.text === ",") {
        out.push(",");
        pendingNewline = false;
        continue;
      }
      if (t.text === "." || t.text === ";") {
        out.push(t.text);
        if (t.text === ";") pendingNewline = true;
        continue;
      }
      if (t.text === "*") {
        // SELECT * / count(*) 紧贴前一个 token；二元乘法由运算符分支处理不了，这里保持简单
        out.push("*");
        pendingNewline = false;
        continue;
      }
      if (OPERATOR_CHARS.has(t.text) || t.text === "-" || t.text === "/") {
        // 运算符：合并两字符运算符（>= <= <> != == || ->），前后留空格
        let op = t.text;
        if (next?.type === "punct" && OPERATOR_CHARS.has(next.text)) {
          const two = t.text + next.text;
          if (["<=", ">=", "<>", "!=", "==", "||", "->"].includes(two)) {
            op = two;
            i++;
          }
        }
        pushSpace();
        out.push(op);
        pendingNewline = false;
        continue;
      }
      out.push(t.text);
      pendingNewline = false;
      continue;
    }

    if (t.type === "string") {
      if (pendingNewline) appendBreak();
      else pushSpace();
      out.push(t.text);
      pendingNewline = false;
      continue;
    }

    // word
    const kw = KEYWORDS.has(t.upper) ? t.upper : t.text;

    let breakBefore = false;
    if (CLAUSE_BREAK.has(kw) && !(kw === "JOIN" && JOIN_MODIFIERS.has(prev?.upper ?? ""))) {
      breakBefore = true;
    }
    if (JOIN_MODIFIERS.has(kw) && next?.type === "word" && next.upper === "JOIN") {
      breakBefore = true; // LEFT / RIGHT / ... JOIN 整体换行
    }
    if (kw === "BY") breakBefore = false;

    if (pendingNewline || breakBefore) {
      if (out.length > 0) appendBreak();
      pendingNewline = false;
    } else {
      pushSpace();
    }
    out.push(kw);
  }

  return out.join("").trim();
}
