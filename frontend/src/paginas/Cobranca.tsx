import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { api } from "../api";
import { cnpj as fmtCNPJ, mesAtual, nomeMes, quantidade, reais } from "../formato";

/**
 * Cobrança mostra a soma do mês por cliente e produto e gera o Excel.
 *
 * Só cupons confirmados entram. Quando há pendentes, a tela diz quantos —
 * fechar uma cobrança sem saber que faltam cupons é o erro mais caro aqui.
 */
export function Cobranca() {
  const [mes, setMes] = useState(mesAtual());
  const [clienteID, setClienteID] = useState(0);

  const clientes = useQuery({ queryKey: ["clientes"], queryFn: api.clientes });
  const cobranca = useQuery({
    queryKey: ["cobranca", mes, clienteID],
    queryFn: () => api.cobranca(mes, clienteID),
  });

  const dados = cobranca.data;

  return (
    <>
      <div className="cartao">
        <h2>Cobrança de {nomeMes(mes)}</h2>

        <div className="linha">
          <div className="estreito">
            <label htmlFor="b-mes">Mês</label>
            <input id="b-mes" type="month" value={mes} onChange={(e) => setMes(e.target.value)} />
          </div>
          <div>
            <label htmlFor="b-cliente">Cliente</label>
            <select
              id="b-cliente"
              value={clienteID}
              onChange={(e) => setClienteID(Number(e.target.value))}
            >
              <option value={0}>Todos os clientes</option>
              {(clientes.data ?? []).map((c) => (
                <option key={c.id} value={c.id}>
                  {c.nome}
                </option>
              ))}
            </select>
          </div>
          <div style={{ flex: "0 0 auto" }}>
            {/* Download direto: o navegador manda o cookie de sessão. */}
            <a className="acao" href={api.urlExcel(mes, clienteID)} style={{
              display: "inline-block", textDecoration: "none", color: "#fff",
            }}>
              Exportar Excel
            </a>
          </div>
        </div>
      </div>

      {cobranca.isError && <div className="erro">{(cobranca.error as Error).message}</div>}
      {cobranca.isLoading && <p className="vazio">Somando…</p>}

      {dados && dados.pendentes > 0 && (
        <div className="aviso">
          <strong>
            {dados.pendentes} cupom(ns) ainda pendente(s) de revisão não entraram nesta
            cobrança.
          </strong>{" "}
          Confirme-os na aba Revisão antes de fechar o mês.
        </div>
      )}

      {dados && dados.linhas.length === 0 && (
        <div className="cartao">
          <p className="vazio">
            Nenhum cupom confirmado em {nomeMes(mes)}
            {dados.pendentes > 0 && ` (há ${dados.pendentes} pendente(s))`}.
          </p>
        </div>
      )}

      {dados && dados.linhas.length > 0 && (
        <>
          <div className="cartao">
            <h2>Resumo por cliente</h2>
            <div className="rolagem">
              <table>
                <thead>
                  <tr>
                    <th>Cliente</th>
                    <th>CNPJ</th>
                    <th className="numero">Cupons</th>
                    <th className="numero">Total</th>
                  </tr>
                </thead>
                <tbody>
                  {dados.subtotais.map((s) => (
                    <tr key={s.cliente_id}>
                      <td style={{ whiteSpace: "normal" }}>{s.cliente}</td>
                      <td>{fmtCNPJ(s.cnpj_cliente)}</td>
                      <td className="numero">{s.num_cupons}</td>
                      <td className="numero">{reais(s.total)}</td>
                    </tr>
                  ))}
                </tbody>
                <tfoot>
                  <tr>
                    <td colSpan={3}>Total geral (cupons confirmados)</td>
                    <td className="numero">{reais(dados.total)}</td>
                  </tr>
                </tfoot>
              </table>
            </div>
          </div>

          <div className="cartao">
            <h2>Por produto</h2>
            <div className="rolagem">
              <table>
                <thead>
                  <tr>
                    <th>Cliente</th>
                    <th>Código</th>
                    <th>Produto</th>
                    <th>Un.</th>
                    <th className="numero">Qtd. total</th>
                    <th className="numero">Cupons</th>
                    <th className="numero">Valor</th>
                  </tr>
                </thead>
                <tbody>
                  {dados.linhas.map((l, i) => (
                    <tr key={i}>
                      <td style={{ whiteSpace: "normal" }}>{l.cliente}</td>
                      <td>{l.codigo || "—"}</td>
                      <td style={{ whiteSpace: "normal" }}>{l.produto}</td>
                      <td>{l.unidade}</td>
                      <td className="numero">{quantidade(l.qtd_total)}</td>
                      <td className="numero">{l.num_cupons}</td>
                      <td className="numero">{reais(l.valor_total)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>
        </>
      )}
    </>
  );
}
