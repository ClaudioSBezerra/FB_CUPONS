import { useEffect, useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  api,
  type Cupom,
  type CupomDetalhe,
  type Item,
  type Sessao,
  type Unidade,
} from "../api";
import { cnpj as fmtCNPJ, dataBR, numeroDeTexto, quantidade, reais } from "../formato";

/**
 * Revisão é onde o olho humano fecha o cupom: foto grande ao lado do
 * formulário, campos incertos em amarelo, totais recalculados ao vivo.
 *
 * Número e data chegam sempre marcados como incertos — são os campos que mais
 * erram em impressão apagada, e a regra é forçar a conferência.
 */
export function Revisao({
  sessao,
  cupomInicial,
}: {
  sessao: Sessao;
  cupomInicial: number | null;
}) {
  const [abertoID, setAbertoID] = useState<number | null>(cupomInicial);

  const pendentes = useQuery({
    queryKey: ["cupons", { status: "pendente" }],
    queryFn: () => api.cupons({ status: "pendente" }),
  });

  // Ao abrir a aba sem cupom escolhido, vai para o primeiro pendente.
  useEffect(() => {
    if (abertoID === null && pendentes.data && pendentes.data.length > 0) {
      setAbertoID(pendentes.data[0]!.id);
    }
  }, [abertoID, pendentes.data]);

  function proximo() {
    const lista = pendentes.data ?? [];
    const i = lista.findIndex((c) => c.id === abertoID);
    const seguinte = lista[i + 1] ?? lista.find((c) => c.id !== abertoID);
    setAbertoID(seguinte ? seguinte.id : null);
  }

  if (pendentes.isLoading) return <p className="vazio">Carregando pendentes…</p>;
  if (pendentes.isError)
    return <div className="erro">{(pendentes.error as Error).message}</div>;

  const lista = pendentes.data ?? [];

  if (lista.length === 0 && abertoID === null) {
    return (
      <div className="cartao">
        <h2>Revisão</h2>
        <p className="vazio">
          Nenhum cupom pendente. Importe um PDF na aba Importar.
        </p>
      </div>
    );
  }

  return (
    <>
      <div className="cartao">
        <div className="linha">
          <div>
            <label htmlFor="pendente">
              Pendentes de revisão ({lista.length})
            </label>
            <select
              id="pendente"
              value={abertoID ?? ""}
              onChange={(e) => setAbertoID(Number(e.target.value))}
            >
              {lista.map((c) => (
                <option key={c.id} value={c.id}>
                  {c.numero || "sem número"} — {c.cliente || "cliente não lido"} —{" "}
                  {dataBR(c.data)} — {reais(c.total)}
                </option>
              ))}
              {abertoID !== null && !lista.some((c) => c.id === abertoID) && (
                <option value={abertoID}>cupom #{abertoID}</option>
              )}
            </select>
          </div>
          <div style={{ flex: "0 0 auto" }}>
            <button className="secundaria" onClick={proximo} disabled={lista.length < 2}>
              Próximo pendente →
            </button>
          </div>
        </div>
      </div>

      {abertoID !== null && (
        <Formulario
          key={abertoID}
          cupomID={abertoID}
          sessao={sessao}
          onConcluido={proximo}
        />
      )}
    </>
  );
}

/** rascunho é o estado editável do formulário. */
interface Rascunho {
  numero: string;
  data: string;
  cliente: string;
  cnpj_cliente: string;
  total: string;
  obs: string;
  itens: ItemRascunho[];
}

interface ItemRascunho {
  cod: string;
  prod: string;
  un: Unidade;
  qtd: string;
  vun: string;
  vtot: string;
  incertos: string[];
}

function rascunhoDe(c: CupomDetalhe): Rascunho {
  return {
    numero: c.numero,
    data: c.data ?? "",
    cliente: c.cliente,
    cnpj_cliente: fmtCNPJ(c.cnpj_cliente),
    total: c.total ? String(c.total.toFixed(2)).replace(".", ",") : "",
    obs: c.obs,
    itens: c.itens.map((i) => ({
      cod: i.cod,
      prod: i.prod,
      un: (i.un || "UND") as Unidade,
      qtd: formatarNumero(i.qtd),
      vun: formatarNumero(i.vun),
      vtot: formatarNumero(i.vtot),
      incertos: i.incertos ?? [],
    })),
  };
}

function formatarNumero(n: number): string {
  if (n === 0) return "";
  return String(n).replace(".", ",");
}

function Formulario({
  cupomID,
  sessao,
  onConcluido,
}: {
  cupomID: number;
  sessao: Sessao;
  onConcluido: () => void;
}) {
  const query = useQueryClient();
  const [rascunho, setRascunho] = useState<Rascunho | null>(null);
  const [zoom, setZoom] = useState(1);
  const [giro, setGiro] = useState(0);
  const [justificativa, setJustificativa] = useState("");

  const cupom = useQuery({
    queryKey: ["cupom", cupomID],
    queryFn: () => api.cupom(cupomID),
  });

  // Carrega o rascunho na primeira vez; não sobrescreve edição em andamento.
  useEffect(() => {
    if (cupom.data && rascunho === null) setRascunho(rascunhoDe(cupom.data));
  }, [cupom.data, rascunho]);

  const salvar = useMutation({
    mutationFn: () => api.salvarCupom(cupomID, corpoDoRascunho(rascunho!)),
    onSuccess: (novo) => {
      query.setQueryData(["cupom", cupomID], novo);
      setRascunho(rascunhoDe(novo));
      query.invalidateQueries({ queryKey: ["cupons"] });
    },
  });

  const confirmar = useMutation({
    mutationFn: (forcar: boolean) => api.confirmar(cupomID, justificativa, forcar),
    onSuccess: () => {
      query.invalidateQueries({ queryKey: ["cupons"] });
      query.invalidateQueries({ queryKey: ["cobranca"] });
      onConcluido();
    },
  });

  const rejeitar = useMutation({
    mutationFn: () => api.rejeitar(cupomID, justificativa),
    onSuccess: () => {
      query.invalidateQueries({ queryKey: ["cupons"] });
      onConcluido();
    },
  });

  const reprocessar = useMutation({
    mutationFn: (forte: boolean) => api.reprocessar(cupomID, forte),
    onSuccess: (novo) => {
      query.setQueryData(["cupom", cupomID], novo);
      setRascunho(rascunhoDe(novo));
    },
  });

  // Totais ao vivo: o operador vê a conta fechar enquanto digita.
  const somaItens = useMemo(() => {
    if (!rascunho) return 0;
    return rascunho.itens.reduce((s, i) => s + (numeroDeTexto(i.vtot) ?? 0), 0);
  }, [rascunho]);

  const totalDigitado = rascunho ? (numeroDeTexto(rascunho.total) ?? 0) : 0;
  const diferenca = Math.round((somaItens - totalDigitado) * 100) / 100;

  if (cupom.isLoading || !rascunho) return <p className="vazio">Carregando cupom…</p>;
  if (cupom.isError) return <div className="erro">{(cupom.error as Error).message}</div>;

  const d = cupom.data!;
  const incerto = (campo: string) => (d.incertos.includes(campo) ? "incerto" : "");

  function atualizar(mudanca: Partial<Rascunho>) {
    setRascunho((r) => (r ? { ...r, ...mudanca } : r));
  }

  function atualizarItem(i: number, mudanca: Partial<ItemRascunho>) {
    setRascunho((r) => {
      if (!r) return r;
      const itens = r.itens.map((it, j) => (j === i ? { ...it, ...mudanca } : it));
      return { ...r, itens };
    });
  }

  /** recalcularItem completa o campo que falta, como faz o backend. */
  function recalcularItem(i: number) {
    setRascunho((r) => {
      if (!r) return r;
      const it = r.itens[i];
      if (!it) return r;
      const qtd = numeroDeTexto(it.qtd);
      const vun = numeroDeTexto(it.vun);
      const vtot = numeroDeTexto(it.vtot);

      let novo = { ...it };
      if (qtd !== null && vun !== null) {
        novo.vtot = formatarNumero(Math.round(qtd * vun * 100) / 100);
      } else if (vtot !== null && vun !== null && vun !== 0) {
        novo.qtd = formatarNumero(Math.round((vtot / vun) * 1000) / 1000);
      } else if (vtot !== null && qtd !== null && qtd !== 0) {
        novo.vun = formatarNumero(Math.round((vtot / qtd) * 100) / 100);
      }
      const itens = r.itens.map((x, j) => (j === i ? novo : x));
      return { ...r, itens };
    });
  }

  return (
    <div className="revisao">
      <div className="foto-area">
        <div className="ferramentas-foto">
          <button className="secundaria" onClick={() => setZoom((z) => Math.min(z + 0.25, 4))}>
            Ampliar +
          </button>
          <button className="secundaria" onClick={() => setZoom((z) => Math.max(z - 0.25, 0.5))}>
            Reduzir −
          </button>
          <button className="secundaria" onClick={() => setZoom(1)}>
            100%
          </button>
          <button className="secundaria" onClick={() => setGiro((g) => (g + 90) % 360)}>
            Girar ↻
          </button>
        </div>

        {d.foto_id ? (
          <img
            src={api.urlFoto(cupomID)}
            alt={`Foto do cupom ${d.numero || cupomID}`}
            style={{ transform: `rotate(${giro}deg) scale(${zoom})` }}
          />
        ) : (
          <p className="vazio">
            Este cupom não tem foto guardada (veio da importação do legado).
          </p>
        )}

        <p className="ajuda">Origem: {d.fonte || "não informada"}</p>
      </div>

      <div>
        <div className="cartao">
          <h2>
            Cupom {d.numero || "sem número"}{" "}
            <span className={`etiqueta ${d.status}`}>{d.status}</span>
          </h2>

          {d.avisos.length > 0 && (
            <div className="aviso">
              <strong>Avisos da leitura</strong>
              <ul>
                {d.avisos.map((a, i) => (
                  <li key={i}>{a}</li>
                ))}
              </ul>
            </div>
          )}

          {d.parecidos && d.parecidos.length > 0 && (
            <div className="aviso">
              <strong>Números parecidos no mês</strong>
              <ul>
                {d.parecidos.map((p, i) => (
                  <li key={i}>{p}</li>
                ))}
              </ul>
            </div>
          )}

          <div className="linha">
            <div className="estreito">
              <label htmlFor="f-numero">Número</label>
              <input
                id="f-numero"
                className={incerto("numero")}
                value={rascunho.numero}
                onChange={(e) => atualizar({ numero: e.target.value })}
              />
            </div>
            <div className="estreito">
              <label htmlFor="f-data">Data</label>
              <input
                id="f-data"
                type="date"
                className={incerto("data")}
                value={rascunho.data}
                onChange={(e) => atualizar({ data: e.target.value })}
              />
            </div>
            <div>
              <label htmlFor="f-cliente">Cliente</label>
              <input
                id="f-cliente"
                className={incerto("cli")}
                value={rascunho.cliente}
                onChange={(e) => atualizar({ cliente: e.target.value })}
              />
            </div>
            <div>
              <label htmlFor="f-cnpj">CNPJ do cliente</label>
              <input
                id="f-cnpj"
                className={incerto("cnpjCli")}
                value={rascunho.cnpj_cliente}
                onChange={(e) => atualizar({ cnpj_cliente: e.target.value })}
              />
            </div>
          </div>

          <h3 style={{ fontSize: "0.95rem", marginBottom: "0.4rem" }}>Itens</h3>
          <div className="rolagem">
            <table>
              <thead>
                <tr>
                  <th>Código</th>
                  <th>Produto</th>
                  <th>Un.</th>
                  <th className="numero">Qtd.</th>
                  <th className="numero">Valor unit.</th>
                  <th className="numero">Valor total</th>
                  <th />
                </tr>
              </thead>
              <tbody>
                {rascunho.itens.map((it, i) => (
                  <tr key={i}>
                    <td style={{ width: "6rem" }}>
                      <input
                        className={it.incertos.includes("cod") ? "incerto" : ""}
                        value={it.cod}
                        onChange={(e) => atualizarItem(i, { cod: e.target.value })}
                        aria-label={`Código do item ${i + 1}`}
                      />
                    </td>
                    <td style={{ minWidth: "16rem" }}>
                      <input
                        className={it.incertos.includes("prod") ? "incerto" : ""}
                        value={it.prod}
                        onChange={(e) => atualizarItem(i, { prod: e.target.value })}
                        aria-label={`Produto do item ${i + 1}`}
                      />
                    </td>
                    <td style={{ width: "5rem" }}>
                      <select
                        value={it.un}
                        onChange={(e) => atualizarItem(i, { un: e.target.value as Unidade })}
                        aria-label={`Unidade do item ${i + 1}`}
                      >
                        <option value="UND">UND</option>
                        <option value="KG">KG</option>
                      </select>
                    </td>
                    <td className="numero" style={{ width: "6rem" }}>
                      <input
                        className={it.incertos.includes("qtd") ? "incerto" : ""}
                        value={it.qtd}
                        onChange={(e) => atualizarItem(i, { qtd: e.target.value })}
                        onBlur={() => recalcularItem(i)}
                        aria-label={`Quantidade do item ${i + 1}`}
                      />
                    </td>
                    <td className="numero" style={{ width: "7rem" }}>
                      <input
                        className={it.incertos.includes("vun") ? "incerto" : ""}
                        value={it.vun}
                        onChange={(e) => atualizarItem(i, { vun: e.target.value })}
                        onBlur={() => recalcularItem(i)}
                        aria-label={`Valor unitário do item ${i + 1}`}
                      />
                    </td>
                    <td className="numero" style={{ width: "7rem" }}>
                      <input
                        className={it.incertos.includes("vtot") ? "incerto" : ""}
                        value={it.vtot}
                        onChange={(e) => atualizarItem(i, { vtot: e.target.value })}
                        aria-label={`Valor total do item ${i + 1}`}
                      />
                    </td>
                    <td>
                      <button
                        className="secundaria"
                        onClick={() =>
                          setRascunho((r) =>
                            r ? { ...r, itens: r.itens.filter((_, j) => j !== i) } : r,
                          )
                        }
                        aria-label={`Remover item ${i + 1}`}
                      >
                        ✕
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
              <tfoot>
                <tr>
                  <td colSpan={5}>Soma dos itens</td>
                  <td className="numero">{reais(somaItens)}</td>
                  <td />
                </tr>
              </tfoot>
            </table>
          </div>

          <button
            className="secundaria"
            style={{ marginTop: "0.5rem" }}
            onClick={() =>
              setRascunho((r) =>
                r
                  ? {
                      ...r,
                      itens: [
                        ...r.itens,
                        { cod: "", prod: "", un: "UND", qtd: "", vun: "", vtot: "", incertos: [] },
                      ],
                    }
                  : r,
              )
            }
          >
            + Acrescentar item
          </button>

          <div className="linha" style={{ marginTop: "1rem" }}>
            <div className="estreito">
              <label htmlFor="f-total">Total impresso no cupom</label>
              <input
                id="f-total"
                className={incerto("total")}
                value={rascunho.total}
                onChange={(e) => atualizar({ total: e.target.value })}
              />
            </div>
            <div style={{ flex: "2 1 20rem" }}>
              {Math.abs(diferenca) <= 0.01 ? (
                <div className="sucesso" style={{ margin: 0 }}>
                  A soma dos itens fecha com o total: {reais(somaItens)}.
                </div>
              ) : (
                <div className="aviso" style={{ margin: 0 }}>
                  Diferença de <strong>{reais(Math.abs(diferenca))}</strong> entre a soma dos
                  itens ({reais(somaItens)}) e o total digitado ({reais(totalDigitado)}).
                </div>
              )}
            </div>
          </div>

          <div style={{ marginTop: "0.75rem" }}>
            <label htmlFor="f-obs">Observação</label>
            <textarea
              id="f-obs"
              rows={2}
              value={rascunho.obs}
              onChange={(e) => atualizar({ obs: e.target.value })}
            />
          </div>
        </div>

        <div className="cartao">
          {d.bloqueios.length > 0 && (
            <div className="aviso">
              <strong>Para confirmar, resolva:</strong>
              <ul>
                {d.bloqueios.map((b, i) => (
                  <li key={i}>{b}</li>
                ))}
              </ul>
            </div>
          )}

          {(salvar.isError || confirmar.isError || rejeitar.isError || reprocessar.isError) && (
            <div className="erro">
              {
                (
                  (salvar.error ?? confirmar.error ?? rejeitar.error ?? reprocessar.error) as Error
                ).message
              }
            </div>
          )}

          <div style={{ marginBottom: "0.75rem" }}>
            <label htmlFor="f-justificativa">
              Justificativa (obrigatória para confirmar com divergência ou rejeitar)
            </label>
            <input
              id="f-justificativa"
              value={justificativa}
              onChange={(e) => setJustificativa(e.target.value)}
            />
          </div>

          <div className="linha">
            <button
              className="acao"
              style={{ flex: "0 0 auto" }}
              disabled={salvar.isPending}
              onClick={() => salvar.mutate()}
            >
              {salvar.isPending ? "Salvando…" : "Salvar edição"}
            </button>

            <button
              className="acao"
              style={{ flex: "0 0 auto" }}
              disabled={confirmar.isPending}
              onClick={() => confirmar.mutate(!d.pode_confirmar)}
            >
              {d.pode_confirmar ? "Confirmar" : "Confirmar com divergência"}
            </button>

            <button
              className="secundaria perigo"
              style={{ flex: "0 0 auto" }}
              disabled={rejeitar.isPending}
              onClick={() => rejeitar.mutate()}
            >
              Rejeitar
            </button>

            <span className="espaco" style={{ flex: 1 }} />

            {sessao.leitura_habilitada && d.foto_id && (
              <>
                <button
                  className="secundaria"
                  style={{ flex: "0 0 auto" }}
                  disabled={reprocessar.isPending}
                  onClick={() => reprocessar.mutate(false)}
                >
                  Reler ({sessao.modelo})
                </button>
                <button
                  className="secundaria"
                  style={{ flex: "0 0 auto" }}
                  disabled={reprocessar.isPending}
                  onClick={() => reprocessar.mutate(true)}
                >
                  Reler com modelo mais forte
                </button>
              </>
            )}
          </div>

          <p className="ajuda" style={{ marginTop: "0.75rem" }}>
            Salvar a edição não confirma o cupom: só cupons confirmados entram na
            cobrança. Itens: {rascunho.itens.length} · Quantidade total:{" "}
            {quantidade(rascunho.itens.reduce((s, i) => s + (numeroDeTexto(i.qtd) ?? 0), 0))}
          </p>
        </div>
      </div>
    </div>
  );
}

/** corpoDoRascunho traduz o formulário para o corpo que a API espera. */
function corpoDoRascunho(r: Rascunho) {
  return {
    numero: r.numero.trim(),
    data: r.data || null,
    cliente: r.cliente.trim(),
    cnpj_cliente: r.cnpj_cliente.replace(/\D/g, ""),
    total: numeroDeTexto(r.total) ?? 0,
    obs: r.obs.trim(),
    itens: r.itens.map((i) => ({
      cod: i.cod.trim(),
      prod: i.prod.trim(),
      un: i.un,
      qtd: numeroDeTexto(i.qtd) ?? 0,
      vun: numeroDeTexto(i.vun) ?? 0,
      vtot: numeroDeTexto(i.vtot) ?? 0,
    })),
  };
}

// Reexportado para os testes de formato do rascunho.
export type { Cupom, Item };
