import { TabulatorFull as Tabulator } from "tabulator-tables";
import "tabulator-tables/dist/css/tabulator.css";
import "./result-grid.css";

const valueText = (value) =>
  value === null ? "NULL" : typeof value === "object" ? JSON.stringify(value) : String(value);
const escapeHTML = (value) => String(value).replace(/[&<>"']/g, (c) => ({
  "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;",
})[c]);
// 单元格值一律是字符串，无法根据内容可靠判断数值（如 "0012"、"1e3"），
// 因此按元数据里的字段类型决定右对齐；BIT 存储的是位串，保持左对齐。
// 类型可能带精度和修饰，如 "bigint(20) unsigned"、"UNSIGNED BIGINT"、"DOUBLE PRECISION"。
const NUMERIC_TYPE = /^(tinyint|smallint|mediumint|int|integer|bigint|decimal|numeric|float|double|real|year)\b/;
const numericColumn = (type) =>
  NUMERIC_TYPE.test(String(type || "").toLowerCase().split("(")[0].replace(/\bunsigned\b/, "").trim());

// SQL columns are positional: duplicate names and names containing dots must
// never become object keys. Values remain strings/null, preserving precision.
window.createResultGrid = (container, columns, rows, skip = 0) => {
  const toolbar = document.createElement("div");
  toolbar.className = "grid-toolbar";
  const hint = document.createElement("span");
  hint.className = "grid-hint";
  hint.textContent = "拖动列边界调整宽度 · 拖动表头换序 · 框选后 Ctrl / ⌘ + C 复制 · 双击单元格查看全文";
  const chooser = document.createElement("details");
  chooser.className = "grid-columns";
  const summary = document.createElement("summary");
  summary.textContent = "显示列";
  chooser.append(summary);
  const choices = document.createElement("div");
  choices.className = "grid-column-options";
  chooser.append(choices);
  const inspect = document.createElement("button");
  inspect.type = "button";
  inspect.className = "button";
  inspect.textContent = "查看单元格";
  inspect.disabled = true;
  const copy = document.createElement("button");
  copy.type = "button";
  copy.className = "button";
  copy.textContent = "复制选区";
  copy.disabled = true;
  const feedback = document.createElement("span");
  feedback.className = "grid-feedback";
  feedback.setAttribute("role", "status");
  toolbar.append(hint, feedback, chooser, inspect, copy);
  const element = document.createElement("div");
  element.setAttribute("aria-label", "SQL 查询结果");
  container.replaceChildren(toolbar, element);

  const dialog = document.createElement("dialog");
  dialog.className = "grid-cell-dialog";
  const heading = document.createElement("h2");
  const content = document.createElement("textarea");
  content.readOnly = true;
  content.setAttribute("aria-label", "单元格完整内容");
  const close = document.createElement("button");
  close.type = "button";
  close.className = "button";
  close.textContent = "关闭";
  close.onclick = () => dialog.close();
  dialog.append(heading, content, close);
  document.body.append(dialog);
  function showCell(cell) {
    if (!cell) return;
    const index = Number(cell.getField().slice(1));
    if (!columns[index]) return;
    heading.textContent = columns[index].name + (cell.getValue() === null ? " · NULL" : "");
    content.value = valueText(cell.getValue());
    dialog.showModal();
    content.focus();
    content.select();
  }
  const grid = new Tabulator(element, {
    height: "400px",
    layout: "fitData",
    data: rows.map((row, i) => Object.fromEntries([
      ["__rowIndex", skip + i + 1], ...columns.map((_, j) => ["c" + j, row[j]]),
    ])),
    placeholder: "没有匹配的数据",
    movableColumns: true,
    selectableRange: 1,
    // Header dragging belongs to column movement; range selection starts in cells.
    selectableRangeColumns: false,
    selectableRangeRows: true,
    selectableRangeClearCells: false,
    clipboard: "copy",
    clipboardCopyStyled: false,
    clipboardCopyRowRange: "range",
    clipboardCopyConfig: { columnHeaders: false, columnGroups: false, rowHeaders: false },
    rowHeader: {
      width: 58, minWidth: 58, maxWidth: 58, frozen: true, resizable: false, headerSort: false,
      formatter: (cell) => String(cell.getRow().getData().__rowIndex),
      clipboard: false, hozAlign: "center",
    },
    columnDefaults: {
      headerSort: false, resizable: true, minWidth: 110, width: 180,
      editable: false,
    },
    columns: columns.map((column, i) => {
      const numeric = numericColumn(column.type);
      return {
        field: "c" + i,
        title: escapeHTML(column.comment || column.name),
        titleFormatter: () => {
          const title = document.createElement("div");
          title.textContent = column.comment || column.name;
          title.title = column.name + " · " + (column.type || "") + (column.comment ? " · " + column.comment : "");
          if (column.comment) {
            const name = document.createElement("small");
            name.textContent = column.name;
            title.append(name);
          }
          return title;
        },
        // 数字列右对齐便于按位比较大小，表头跟随列内容对齐
        hozAlign: numeric ? "right" : "left",
        headerHozAlign: numeric ? "right" : "left",
        formatter: (cell) => {
          const value = cell.getValue();
          const span = document.createElement("span");
          span.textContent = value === "" ? '\"\"' : valueText(value);
          if (value === null) span.className = "cell-null";
          return span;
        },
        accessorClipboard: (value) => valueText(value),
      };
    }),
  });
  const firstCell = () => grid.getRanges()[0]?.getCells()[0]?.find((cell) => cell.getField()?.startsWith("c"));
  const updateSelection = () => {
    copy.disabled = !firstCell();
    inspect.disabled = !firstCell();
    feedback.textContent = "";
  };
  grid.on("rangeAdded", updateSelection);
  grid.on("rangeChanged", updateSelection);
  grid.on("cellDblClick", (_event, cell) => showCell(cell));
  grid.on("cellTapHold", (_event, cell) => showCell(cell));
  grid.on("clipboardCopied", () => { feedback.textContent = "已复制"; });
  grid.on("clipboardCopyError", () => { feedback.textContent = "复制失败，请框选后按 Ctrl / ⌘ + C"; });
  copy.onclick = () => grid.copyToClipboard("range");
  inspect.onclick = () => showCell(firstCell());
  element.addEventListener("keydown", (event) => {
    if (event.key === "Enter" && !dialog.open) {
      event.preventDefault();
      showCell(firstCell());
    }
  });
  columns.forEach((column, i) => {
    const label = document.createElement("label");
    const input = document.createElement("input");
    input.type = "checkbox";
    input.checked = true;
    input.onchange = () => {
      // Keep one data column visible so keyboard selection remains usable.
      if (!input.checked && choices.querySelectorAll("input:checked").length === 0) {
        input.checked = true;
        feedback.textContent = "至少保留一列";
        return;
      }
      grid.getColumn("c" + i).toggle();
    };
    label.append(input, document.createTextNode(column.name + (column.comment ? " · " + column.comment : "")));
    choices.append(label);
  });
  return { destroy() { grid.destroy(); dialog.remove(); } };
};
