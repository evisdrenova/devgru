import * as vscode from "vscode";
import * as WebSocket from "ws";
import * as path from "path";

interface DevGruMessage {
  type: string;
  timestamp: string;
  data: any;
}

interface SelectionData {
  type: "selection";
  file: string;
  text: string;
  start_line: number;
  end_line: number;
  language?: string;
}

interface DiagnosticData {
  type: "diagnostic";
  file: string;
  message: string;
  line: number;
  column: number;
  severity: string;
}

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

  // Watch for DevGru handshake in terminal
  const terminalListener = vscode.window.onDidWriteTerminalData((e) => {
    devgruClient.handleTerminalOutput(e.data);
  });

  // Register disposables
  context.subscriptions.push(
    openPanelCommand,
    insertFileRefCommand,
    runPromptCommand,
    selectionListener,
    diagnosticListener,
    terminalListener,
    devgruClient
  );

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
  private readonly HANDSHAKE_MESSAGE = "###DEVGRU_VSCODE_HANDSHAKE###";
  private readonly DIFF_START_MARKER = "<<<DEVGRU_DIFF_START>>>";
  private readonly DIFF_END_MARKER = "<<<DEVGRU_DIFF_END>>>";

  constructor() {
    // Try to connect initially
    this.tryConnect();
  }

  async tryConnect(): Promise<void> {
    const config = vscode.workspace.getConfiguration("devgru");
    const port = config.get("serverPort", 8123);

    if (this.ws && this.ws.readyState === WebSocket.OPEN) {
      return; // Already connected
    }

    try {
      this.ws = new WebSocket(`ws://127.0.0.1:${port}/ws`);

      this.ws.on("open", () => {
        console.log("Connected to DevGru server");
        vscode.window.showInformationMessage("DevGru: Connected to server");
        this.sendWorkspaceInfo();

        // Clear reconnect timer
        if (this.reconnectTimer) {
          clearTimeout(this.reconnectTimer);
          this.reconnectTimer = null;
        }
      });

      this.ws.on("message", (data: WebSocket.Data) => {
        try {
          const message: DevGruMessage = JSON.parse(data.toString());
          this.handleServerMessage(message);
        } catch (e) {
          console.error("Failed to parse DevGru message:", e);
        }
      });

      this.ws.on("close", () => {
        console.log("Disconnected from DevGru server");
        this.ws = null;
        this.scheduleReconnect();
      });

      this.ws.on("error", (error) => {
        console.error("DevGru WebSocket error:", error);
        this.ws = null;
        this.scheduleReconnect();
      });
    } catch (error) {
      console.error("Failed to connect to DevGru server:", error);
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
      this.ws.send(JSON.stringify(message));
    }
  }

  private sendWorkspaceInfo(): void {
    const workspaceFolders = vscode.workspace.workspaceFolders;
    const openFiles = vscode.workspace.textDocuments
      .filter((doc) => !doc.isUntitled)
      .map((doc) => vscode.workspace.asRelativePath(doc.uri));

    this.sendMessage({
      type: "workspace",
      timestamp: new Date().toISOString(),
      data: {
        root: workspaceFolders?.[0]?.uri.fsPath || "",
        open_files: openFiles,
      },
    });
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
}

export function deactivate() {
  console.log("DevGru extension deactivated");
}
