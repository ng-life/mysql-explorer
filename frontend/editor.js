import { format } from "sql-formatter";
import { EditorState, Compartment } from "@codemirror/state";
import {
  EditorView,
  keymap,
  lineNumbers,
  highlightActiveLine,
  drawSelection,
} from "@codemirror/view";
import {
  defaultKeymap,
  history,
  historyKeymap,
  isolateHistory,
} from "@codemirror/commands";
import {
  syntaxHighlighting,
  defaultHighlightStyle,
  bracketMatching,
} from "@codemirror/language";
import {
  autocompletion,
  completionKeymap,
  startCompletion,
} from "@codemirror/autocomplete";
import { sql, MySQL } from "@codemirror/lang-sql";

// The page owns query state; CodeMirror owns only editing and completion state.
window.createSQLEditor = (parent, execute) => {
  const editable = new Compartment();
  const language = new Compartment();
  const view = new EditorView({
    parent,
    state: EditorState.create({
      extensions: [
        lineNumbers(),
        history(),
        drawSelection(),
        highlightActiveLine(),
        bracketMatching(),
        syntaxHighlighting(defaultHighlightStyle),
        language.of(sql({ dialect: MySQL, upperCaseKeywords: true })),
        autocompletion(),
        editable.of(EditorView.editable.of(false)),
        keymap.of([
          {
            key: "Mod-Enter",
            run: () => {
              execute();
              return true;
            },
          },
          { key: "Ctrl-Space", run: startCompletion },
          ...completionKeymap,
          ...defaultKeymap,
          ...historyKeymap,
        ]),
        EditorView.contentAttributes.of({
          id: "sql-input",
          "aria-label": "SQL 编辑器",
          role: "textbox",
          "aria-multiline": "true",
          spellcheck: "false",
        }),
        EditorView.lineWrapping,
        EditorView.theme({
          "&": {
            border: "1px solid #d0d5dd",
            borderRadius: "8px",
            backgroundColor: "#f9fafb",
            fontSize: "13px",
            overflow: "hidden",
          },
          "&.cm-focused": {
            outline: "2px solid #d1e0ff",
            borderColor: "#1677ff",
          },
          ".cm-scroller": {
            fontFamily: "ui-monospace, monospace",
            minHeight: "150px",
            maxHeight: "320px",
            overflow: "auto",
          },
          ".cm-content": { padding: "12px 0" },
          ".cm-gutters": {
            backgroundColor: "#f2f4f7",
            color: "#98a2b3",
            border: "none",
          },
          ".cm-line": { padding: "0 12px" },
          ".cm-tooltip-autocomplete": { fontSize: "12px" },
        }),
      ],
    }),
  });
  return {
    get value() {
      return view.state.doc.toString();
    },
    set value(value) {
      view.dispatch({
        changes: { from: 0, to: view.state.doc.length, insert: value },
      });
    },
    set disabled(value) {
      view.dispatch({
        effects: editable.reconfigure(EditorView.editable.of(!value)),
      });
    },
    format() {
      const text = view.state.doc.toString();
      if (!text.trim()) return;
      const formatted = format(text, {
        language: "mysql",
        keywordCase: "upper",
        tabWidth: 2,
      });
      if (formatted !== text) {
        // A single editor transaction keeps formatting undoable.
        view.dispatch({
          annotations: isolateHistory.of("full"),
          changes: { from: 0, to: view.state.doc.length, insert: formatted },
        });
      }
      view.focus();
    },
    setSchema(schema, database, table) {
      view.dispatch({
        effects: language.reconfigure(
          sql({
            dialect: MySQL,
            upperCaseKeywords: true,
            schema,
            defaultSchema: database,
            defaultTable: table,
          }),
        ),
      });
    },
  };
};
