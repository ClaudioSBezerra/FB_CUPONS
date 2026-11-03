import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { api, type Status } from "../api";
import { dataBR, mesAtual, reais } from "../formato";

/** Cupons lista e filtra tudo o que já entrou no sistema. */
export function Cupons({ onAbrir }: { onAbrir: (id: number) => void }) {
  const [mes, setMes] = useState(mesAtual());
  const [clienteID, setClienteID] = useState(0);
  const [status, setStatus] = useState<Status | "">("");
  const [numero, setNumero] = useState("");

  const clientes = useQuery({ queryKey: ["clientes"], queryFn: api.clientes });

  const cupons = useQuery({
    queryKey: ["cupons", { mes, clienteID, status, numero }],
    queryFn: () =>
      api.cupons({
        mes,
        cliente_id: clienteID || undefined,
        status: status || undefined,
        numero: numero || undefined,
      }),
  });

  const lista = cupons.data ?? [];
  const total = lista.reduce((s, c) => s + c.total, 0);

  return (
    <div className="cartao">
      <h2>Cupons</h2>

      <div className="linha" style={{ marginBottom: "1rem" }}>
        <div className="estreito">
          <label htmlFor="c-mes">Mês</label>
          <input id="c-mes" type="month" value={mes} onChange={(e) => setMes(e.target.value)} />
        </div>
        <div>
          <label htmlFor="c-cliente">Cliente</label>
          <select
            id="c-cliente"
            value={clienteID}
            onChange={(e) => setClienteID(Number(e.target.value))}
          >
            <option value={0}>Todos</option>
            {(clientes.data ?? []).map((c) => (
              <option key={c.id} value={c.id}>
                {c.nome}
              </option>
            ))}
          </select>
        </div>
        <div className="estreito">
          <label htmlFor="c-status">Situação</label>
          <select
            id="c-status"
            value={status}
            onChange={(e) => setStatus(e.target.value as Status | "")}
          >
            <option value="">Todas</option>
            <option value="pendente">Pendente</option>
            <option value="confirmado">Confirmado</option>
            <option value="rejeitado">Rejeitado</option>
          </select>
        </div>
        <div className="estreito">
          <label htmlFor="c-numero">Nº do cupom</label>
          <input id="c-numero" value={numero} onChange={(e) => setNumero(e.target.value)} />
        </div>
      </div>

      {cupons.isError && <div className="erro">{(cupons.error as Error).message}</div>}
      {cupons.isLoading && <p className="vazio">Carregando…</p>}

      {!cupons.isLoading && lista.length === 0 && (
        <p className="vazio">Nenhum cupom com esses filtros.</p>
      )}

      {lista.length > 0 && (
        <div className="rolagem">
          <table>
            <thead>
              <tr>
                <th>Nº</th>
                <th>Data</th>
                <th>Cliente</th>
                <th>Situação</th>
                <th className="numero">Total</th>
                <th>Avisos</th>
                <th />
              </tr>
            </thead>
            <tbody>
              {lista.map((c) => (
                <tr key={c.id}>
                  <td>{c.numero || "—"}</td>
                  <td>{dataBR(c.data)}</td>
                  <td style={{ whiteSpace: "normal" }}>{c.cliente || "—"}</td>
                  <td>
                    <span className={`etiqueta ${c.status}`}>{c.status}</span>
                  </td>
                  <td className="numero">{reais(c.total)}</td>
                  <td className="numero">{c.avisos.length || ""}</td>
                  <td>
                    <button className="secundaria" onClick={() => onAbrir(c.id)}>
                      Abrir
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
            <tfoot>
              <tr>
                <td colSpan={4}>{lista.length} cupom(ns)</td>
                <td className="numero">{reais(total)}</td>
                <td colSpan={2} />
              </tr>
            </tfoot>
          </table>
        </div>
      )}
    </div>
  );
}
