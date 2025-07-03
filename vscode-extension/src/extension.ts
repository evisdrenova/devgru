import * as vscode from "vscode";
import WebSocket from "ws";
import * as path from "path";

export function activate(context: vscode.ExtensionContext) {
  console.log("DevGru extension is now active!");

  const devgruClient = new DevGruClient();

  // Register commands
  const openPanelCommand = vscode.commands.registerCommand(
    "devgru.openPanel",
    () => {
      devgruClient.triggerDevGru();
    }
  );

  const insertFileRefCommand = vscode.commands.registerCommand(
    "devgru.insertFileReference",
    () => {
      devgruClient.insertFileReference();
    }
  );

  const runPromptCommand = vscode.commands.registerCommand(
    "devgru.runPrompt",
    async () => {
      const prompt = await vscode.window.showInputBox({
        prompt: "Enter your prompt for DevGru",
        placeHolder: "What would you like DevGru to help with?",
      });

      if (prompt) {
        devgruClient.runPrompt(prompt);
      }
    }
  );

  // Register event listeners
  const selectionListener = vscode.window.onDidChangeTextEditorSelection(
    (e) => {
      devgruClient.handleSelectionChange(e);
    }
  );

  const diagnosticListener = vscode.languages.onDidChangeDiagnostics((e) => {
    devgruClient.handleDiagnosticsChange(e);
  });

  const activeEditorListener = vscode.window.onDidChangeActiveTextEditor(
    (editor) => {
      devgruClient.handleActiveEditorChange(editor);
    }
  );

  // Watch for DevGru handshake in terminal output
  let terminalListener: vscode.Disposable | undefined;

  try {
    // Try to use the newer API if available
    if ("onDidWriteTerminalData" in vscode.window) {
      const onDidWriteTerminalData = (vscode.window as any)
        .onDidWriteTerminalData;
      terminalListener = onDidWriteTerminalData((e: any) => {
        devgruClient.handleTerminalOutput(e.data);
      });
    }
  } catch (error) {
    console.log(
      "Terminal output monitoring not available in this VS Code version"
    );
  }

  const disposables = [
    openPanelCommand,
    insertFileRefCommand,
    runPromptCommand,
    selectionListener,
    diagnosticListener,
    activeEditorListener,
    devgruClient,
  ];

  if (terminalListener) {
    disposables.push(terminalListener);
  }

  context.subscriptions.push(...disposables);

  // Auto-connect if enabled
  const config = vscode.workspace.getConfiguration("devgru");
  if (config.get("autoConnect", true)) {
    setTimeout(() => devgruClient.tryConnect(), 1000);
  }
}

class DevGruClient implements vscode.Disposable {
  private ws: WebSocket | null = null;
  private reconnectTimer: NodeJS.Timeout | null = null;
  private lastSelectionTime = 0;
  private currentPort: number = 8123;
  private readonly HANDSHAKE_MESSAGE = "###DEVGRU_VSCODE_HANDSHAKE###";
  private readonly DIFF_START_MARKER = "<<<DEVGRU_DIFF_START>>>";
  private readonly DIFF_END_MARKER = "<<<DEVGRU_DIFF_END>>>";

  constructor() {
    this.tryConnect();
  }

  private calculateWorkspacePort(): number {
    const workspaceFolders = vscode.workspace.workspaceFolders;
    if (!workspaceFolders || workspaceFolders.length === 0) {
      return 8123; // default port
    }

    const workspacePath = workspaceFolders[0].uri.fsPath;
    const hash = this.simpleHash(workspacePath);

    // Same port range as Go: 8123-8200
    return 8123 + (hash % 77);
  }

  private simpleHash(s: string): number {
    let hash = 0;
    for (let i = 0; i < s.length; i++) {
      hash = hash * 31 + s.charCodeAt(i);
    }
    return Math.abs(hash);
  }

  async tryConnect(): Promise<void> {
    this.currentPort = this.calculateWorkspacePort();

    if (this.ws && this.ws.readyState === WebSocket.OPEN) return;

    try {
      const ws = new WebSocket(`ws://127.0.0.1:${this.currentPort}/ws`);
      this.ws = ws;

      ws.on("open", () => {
        console.log("Connected to DevGru");
        vscode.window.showInformationMessage("DevGru: Connected to server");

        // Send workspace info first
        this.sendWorkspaceInfo();

        // Then send current active file with delays to ensure server processes them
        setTimeout(() => {
          console.log("Sending current active file after connection...");
          this.sendCurrentActiveFile();
        }, 200);

        // Send again after a longer delay as backup
        setTimeout(() => {
          console.log("Sending current active file (backup)...");
          this.sendCurrentActiveFile();
        }, 1000);

        if (this.reconnectTimer) clearTimeout(this.reconnectTimer);
      });

      ws.on("message", (data) => {
        try {
          const msg: DevGruMessage = JSON.parse(data.toString());
          this.handleServerMessage(msg);
        } catch (e) {
          console.error("Failed to parse DevGru message:", e);
        }
      });

      ws.on("close", () => {
        console.log("DevGru WS closed");
        this.ws = null;
        this.scheduleReconnect();
      });

      ws.on("error", (err) => {
        console.error("DevGru WS error:", err);
        this.ws = null;
        this.scheduleReconnect();
      });
    } catch (err) {
      console.error("Failed to connect to DevGru server:", err);
      this.scheduleReconnect();
    }
  }

  private scheduleReconnect(): void {
    if (this.reconnectTimer) {
      return; // Already scheduled
    }

    this.reconnectTimer = setTimeout(() => {
      this.reconnectTimer = null;
      this.tryConnect();
    }, 5000); // Retry every 5 seconds
  }

  private sendMessage(message: DevGruMessage): void {
    if (this.ws && this.ws.readyState === WebSocket.OPEN) {
      console.log("Sending message to DevGru:", message.type, message.data);
      this.ws.send(JSON.stringify(message));
    } else {
      console.log("Cannot send message - WebSocket not connected");
    }
  }

  private sendWorkspaceInfo(): void {
    const workspaceFolders = vscode.workspace.workspaceFolders;
    const openFiles = vscode.workspace.textDocuments
      .filter((doc) => !doc.isUntitled)
      .map((doc) => vscode.workspace.asRelativePath(doc.uri));

    console.log("Sending workspace info - open files:", openFiles);

    this.sendMessage({
      type: "workspace",
      timestamp: new Date().toISOString(),
      data: {
        root: workspaceFolders?.[0]?.uri.fsPath || "",
        open_files: openFiles,
      },
    });
  }

  private sendCurrentActiveFile(): void {
    console.log("sendCurrentActiveFile called");

    const activeEditor = vscode.window.activeTextEditor;
    console.log("Active editor:", activeEditor ? "found" : "not found");

    if (activeEditor && !activeEditor.document.isUntitled) {
      const relativePath = vscode.workspace.asRelativePath(
        activeEditor.document.uri
      );

      console.log("Sending current active file:", relativePath);
      console.log("Document language:", activeEditor.document.languageId);
      console.log("WebSocket state:", this.ws?.readyState);

      // Send file change message
      this.sendMessage({
        type: "fileChange",
        timestamp: new Date().toISOString(),
        data: {
          file: relativePath,
          language: activeEditor.document.languageId,
        },
      });

      // Also send current selection if any
      const selection = activeEditor.selection;
      if (!selection.isEmpty) {
        const selectedText = activeEditor.document.getText(selection);

        console.log(
          "Sending current selection:",
          selectedText.substring(0, 50) + "..."
        );

        this.sendMessage({
          type: "selection",
          timestamp: new Date().toISOString(),
          data: {
            type: "selection",
            file: relativePath,
            text: selectedText,
            start_line: selection.start.line + 1,
            end_line: selection.end.line + 1,
            language: activeEditor.document.languageId,
          },
        });
      }
    } else {
      console.log(
        "No active file to send - activeEditor:",
        !!activeEditor,
        "isUntitled:",
        activeEditor?.document.isUntitled
      );
    }
  }

  handleSelectionChange(event: vscode.TextEditorSelectionChangeEvent): void {
    const now = Date.now();

    // Throttle selection changes (250ms)
    if (now - this.lastSelectionTime < 250) {
      return;
    }
    this.lastSelectionTime = now;

    const editor = event.textEditor;
    const selection = editor.selection;

    if (selection.isEmpty) {
      return; // No selection
    }

    const selectedText = editor.document.getText(selection);
    if (selectedText.trim().length === 0) {
      return; // Empty selection
    }

    const relativePath = vscode.workspace.asRelativePath(editor.document.uri);

    const selectionData: SelectionData = {
      type: "selection",
      file: relativePath,
      text: selectedText,
      start_line: selection.start.line + 1, // VS Code is 0-indexed
      end_line: selection.end.line + 1,
      language: editor.document.languageId,
    };

    this.sendMessage({
      type: "selection",
      timestamp: new Date().toISOString(),
      data: selectionData,
    });
  }

  handleDiagnosticsChange(event: vscode.DiagnosticChangeEvent): void {
    const config = vscode.workspace.getConfiguration("devgru");
    if (!config.get("enableDiagnostics", true)) {
      return;
    }

    // Send top 5 most severe diagnostics
    for (const uri of event.uris) {
      const diagnostics = vscode.languages.getDiagnostics(uri);
      const topDiagnostics = diagnostics
        .filter((d) => d.severity <= vscode.DiagnosticSeverity.Warning)
        .slice(0, 5);

      for (const diagnostic of topDiagnostics) {
        const relativePath = vscode.workspace.asRelativePath(uri);

        const diagnosticData: DiagnosticData = {
          type: "diagnostic",
          file: relativePath,
          message: diagnostic.message,
          line: diagnostic.range.start.line + 1,
          column: diagnostic.range.start.character + 1,
          severity: this.getSeverityString(diagnostic.severity),
        };

        this.sendMessage({
          type: "diagnostic",
          timestamp: new Date().toISOString(),
          data: diagnosticData,
        });
      }
    }
  }

  private getSeverityString(severity: vscode.DiagnosticSeverity): string {
    switch (severity) {
      case vscode.DiagnosticSeverity.Error:
        return "error";
      case vscode.DiagnosticSeverity.Warning:
        return "warning";
      case vscode.DiagnosticSeverity.Information:
        return "info";
      case vscode.DiagnosticSeverity.Hint:
        return "hint";
      default:
        return "info";
    }
  }

  handleTerminalOutput(data: string): void {
    if (data.includes(this.HANDSHAKE_MESSAGE)) {
      // DevGru server detected, try to connect
      vscode.window.showInformationMessage(
        "DevGru server detected! Connecting..."
      );
      setTimeout(() => this.tryConnect(), 500);
    }

    // Handle diff markers
    if (data.includes(this.DIFF_START_MARKER)) {
      // TODO: Capture diff content and show in VS Code diff viewer
      vscode.window.showInformationMessage(
        "DevGru generated a diff - check terminal for details"
      );
    }
  }

  private handleServerMessage(message: DevGruMessage): void {
    switch (message.type) {
      case "diff":
        this.handleDiffMessage(message.data);
        break;
      case "status":
        vscode.window.showInformationMessage(`DevGru: ${message.data.message}`);
        break;
      default:
        console.log("Unknown message type from DevGru:", message.type);
    }
  }

  private async handleDiffMessage(data: any): Promise<void> {
    try {
      const { file, orig_content, new_content } = data;

      // Create temporary files for diff view
      const workspaceFolder = vscode.workspace.workspaceFolders?.[0];
      if (!workspaceFolder) {
        return;
      }

      const originalUri = vscode.Uri.file(
        path.join(workspaceFolder.uri.fsPath, file)
      );
      const tempUri = vscode.Uri.parse(`untitled:${file} (DevGru Suggestion)`);

      // Create document with new content
      const doc = await vscode.workspace.openTextDocument({
        content: new_content,
        language: path.extname(file).substring(1) || "text",
      });

      // Show diff
      await vscode.commands.executeCommand(
        "vscode.diff",
        originalUri,
        doc.uri,
        `${file} ↔ DevGru Suggestion`,
        { preview: true }
      );
    } catch (error) {
      console.error("Failed to show diff:", error);
      vscode.window.showErrorMessage("Failed to display DevGru diff");
    }
  }

  triggerDevGru(): void {
    // Focus on terminal and send a trigger command
    const terminal = this.getOrCreateDevGruTerminal();
    terminal.show();

    vscode.window.showInformationMessage(
      "DevGru panel triggered! Use the terminal to interact."
    );
  }

  async insertFileReference(): Promise<void> {
    const editor = vscode.window.activeTextEditor;
    if (!editor) {
      vscode.window.showWarningMessage("No active editor");
      return;
    }

    const document = editor.document;
    const selection = editor.selection;
    const relativePath = vscode.workspace.asRelativePath(document.uri);

    let reference = `@${relativePath}`;

    if (!selection.isEmpty) {
      const startLine = selection.start.line + 1;
      const endLine = selection.end.line + 1;

      if (startLine === endLine) {
        reference += `#L${startLine}`;
      } else {
        reference += `#L${startLine}-L${endLine}`;
      }
    }

    // Insert the reference at cursor
    await editor.edit((editBuilder) => {
      editBuilder.insert(editor.selection.active, reference);
    });
  }

  async runPrompt(prompt: string): Promise<void> {
    const terminal = this.getOrCreateDevGruTerminal();
    terminal.show();
    terminal.sendText(`devgru run "${prompt.replace(/"/g, '\\"')}"`);
  }

  private getOrCreateDevGruTerminal(): vscode.Terminal {
    // Look for existing DevGru terminal
    const existing = vscode.window.terminals.find((t) => t.name === "DevGru");
    if (existing) {
      return existing;
    }

    // Create new terminal
    return vscode.window.createTerminal({
      name: "DevGru",
      cwd: vscode.workspace.workspaceFolders?.[0]?.uri.fsPath,
    });
  }

  dispose(): void {
    if (this.ws) {
      this.ws.close();
    }
    if (this.reconnectTimer) {
      clearTimeout(this.reconnectTimer);
    }
  }

  handleActiveEditorChange(editor: vscode.TextEditor | undefined): void {
    if (!editor || editor.document.isUntitled) {
      return;
    }

    const relativePath = vscode.workspace.asRelativePath(editor.document.uri);
    const language = editor.document.languageId;

    console.log("Active editor changed to:", relativePath);

    // Send file change notification
    this.sendMessage({
      type: "fileChange",
      timestamp: new Date().toISOString(),
      data: {
        file: relativePath,
        language: language,
      },
    });

    // Also send current selection if any
    const selection = editor.selection;
    if (!selection.isEmpty) {
      this.handleSelectionChange({
        textEditor: editor,
        selections: [selection],
        kind: vscode.TextEditorSelectionChangeKind.Command,
      });
    }
  }
}

export function deactivate() {
  console.log("DevGru extension deactivated");
}
