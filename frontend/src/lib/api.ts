// Typed access to the generated wails bindings. The generated models are
// stricter/looser in places than the UI needs, so every result is coerced to
// the hand-written interfaces in types.ts at this boundary.
import * as $conn from '../../bindings/manticoresearch-gui/connectionservice.js'
import * as $query from '../../bindings/manticoresearch-gui/queryservice.js'
import * as $table from '../../bindings/manticoresearch-gui/tableservice.js'
import type {
  BrowseOptions,
  Connection,
  MutationResult,
  QueryResult,
  TableInfo,
  TestResult,
} from './types'

// The backend may omit columns/rows for non-tabular results; normalize so the
// UI can rely on non-null arrays.
function fixQR(p: { then(onFulfilled: (r: any) => void): unknown }): Promise<QueryResult | null> {
  return Promise.resolve(p).then((r: any) =>
    r ? { ...r, columns: r.columns ?? [], rows: r.rows ?? [] } : r,
  )
}

export const ConnectionService = {
  ListConnections: (): Promise<Connection[]> =>
    $conn.ListConnections() as unknown as Promise<Connection[]>,
  SaveConnection: (c: Connection): Promise<Connection> =>
    $conn.SaveConnection(c as any) as unknown as Promise<Connection>,
  DeleteConnection: (id: string): Promise<void> => $conn.DeleteConnection(id),
  TestConnection: (c: Connection): Promise<TestResult | null> =>
    $conn.TestConnection(c as any) as unknown as Promise<TestResult | null>,
  ClearSSHHostKey: (host: string, port: number): Promise<void> =>
    $conn.ClearSSHHostKey(host, port),
  PickKeyFile: (): Promise<string> => $conn.PickKeyFile() as unknown as Promise<string>,
}

export const QueryService = {
  ExecuteSQL: (connID: string, sql: string): Promise<QueryResult | null> =>
    fixQR($query.ExecuteSQL(connID, sql)),
}

export const TableService = {
  ListTables: (connID: string): Promise<TableInfo[] | null> =>
    $table.ListTables(connID) as unknown as Promise<TableInfo[] | null>,
  DescribeTable: (connID: string, table: string): Promise<QueryResult | null> =>
    fixQR($table.DescribeTable(connID, table)),
  ShowCreateTable: (connID: string, table: string): Promise<QueryResult | null> =>
    fixQR($table.ShowCreateTable(connID, table)),
  TruncateTable: (connID: string, table: string): Promise<QueryResult | null> =>
    fixQR($table.TruncateTable(connID, table)),
  DropTable: (connID: string, table: string): Promise<QueryResult | null> =>
    fixQR($table.DropTable(connID, table)),
  BrowseDocuments: (connID: string, opts: BrowseOptions): Promise<QueryResult | null> =>
    fixQR($table.BrowseDocuments(connID, opts as any)),
  GetDocument: (connID: string, table: string, id: string): Promise<QueryResult | null> =>
    fixQR($table.GetDocument(connID, table, id)),
  InsertDocument: (
    connID: string,
    table: string,
    id: string,
    doc: Record<string, any> | null,
  ): Promise<MutationResult | null> =>
    $table.InsertDocument(connID, table, id, doc as any) as unknown as Promise<MutationResult | null>,
  ReplaceDocument: (
    connID: string,
    table: string,
    id: string,
    doc: Record<string, any>,
  ): Promise<MutationResult | null> =>
    $table.ReplaceDocument(connID, table, id, doc as any) as unknown as Promise<MutationResult | null>,
  UpdateDocument: (
    connID: string,
    table: string,
    id: string,
    doc: Record<string, any>,
  ): Promise<MutationResult | null> =>
    $table.UpdateDocument(connID, table, id, doc as any) as unknown as Promise<MutationResult | null>,
  DeleteDocument: (connID: string, table: string, id: string): Promise<MutationResult | null> =>
    $table.DeleteDocument(connID, table, id) as unknown as Promise<MutationResult | null>,
}

export function errText(e: unknown): string {
  if (e == null) return String(e)
  if (typeof e === 'string') return e
  if (e instanceof Error) return e.message
  return String((e as any).message ?? e)
}
