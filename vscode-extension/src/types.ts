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
