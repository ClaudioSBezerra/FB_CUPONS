import { useRef, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api, type Importacao, type Sessao } from "../api";
import { mesAtual, nomeMes, reais } from "../formato";

/**
 * Importar é a tela que atende ao fluxo principal: arrastar o PDF escaneado
 * no iPhone e deixar a leitura por visão do Claude fazer o resto.
 *
 * Cada página do PDF é um cupom. O resultado vem página a página, com os
 * avisos de cada um, para o operador saber o que vai precisar de revisão.
 */
export function Importar({
  sessao,
  onRevisar,
}: {
  sessao: Sessao;
  onRevisar: (cupomID: number) => void;
}) {
  const [arquivos, setArquivos] = useState<File[]>([]);
  const [mes, setMes] = useState(mesAtual());
  const [arrastando, setArrastando] = useState(false);
  const entrada = useRef<HTMLInputElement>(null);
  const query = useQueryClient();

  const importar = useMutation<Importacao>({
    mutationFn: () => api.importar(arquivos, mes),
    onSuccess: () => {
      setArquivos([]);
      if (entrada.current) entrada.current.value = "";
      // A lista de pendentes e o catálogo mudaram.
      query.invalidateQueries({ queryKey: ["cupons"] });
      query.invalidateQueries({ queryKey: ["produtos"] });
    },
  });

  function acrescentar(novos: FileList | null) {
    if (!novos) return;
    const aceitos = Array.from(novos).filter(
      (f) => f.type === "application/pdf" || f.type.startsWith("image/"),
    );
    setArquivos((atuais) => [...atuais, ...aceitos]);
  }

  const totalMB = arquivos.reduce((s, f) => s + f.size, 0) / 1e6;

  return (
    <>
      {!sessao.leitura_habilitada && (
        <div className="erro">
          A leitura automática está desligada: falta <code>ANTHROPIC_API_KEY</code> no
          servidor. Revisão, cobrança e exportação continuam funcionando.
        </div>
      )}

      <div className="cartao">
        <h2>Importar cupons</h2>
        <p className="ajuda">
          Arraste o PDF escaneado (uma página por cupom) ou fotos soltas. Cada página é
          lida pelo Claude por visão e entra como <strong>pendente de revisão</strong>.
        </p>

        <div
          className={`solta ${arrastando ? "ativa" : ""}`}
          onClick={() => entrada.current?.click()}
          onDragOver={(e) => {
            e.preventDefault();
            setArrastando(true);
          }}
          onDragLeave={() => setArrastando(false)}
          onDrop={(e) => {
            e.preventDefault();
            setArrastando(false);
            acrescentar(e.dataTransfer.files);
          }}
          role="button"
          tabIndex={0}
          onKeyDown={(e) => {
            if (e.key === "Enter" || e.key === " ") entrada.current?.click();
          }}
        >
          <strong>Solte os arquivos aqui</strong>
          <br />
          ou clique para escolher — PDF, JPG ou PNG
        </div>

        <input
          ref={entrada}
          type="file"
          multiple
          accept="application/pdf,image/*"
          hidden
          onChange={(e) => acrescentar(e.target.files)}
        />

        {arquivos.length > 0 && (
          <>
            <table style={{ marginTop: "1rem" }}>
              <thead>
                <tr>
                  <th>Arquivo</th>
                  <th className="numero">Tamanho</th>
                  <th />
                </tr>
              </thead>
              <tbody>
                {arquivos.map((f, i) => (
                  <tr key={`${f.name}-${i}`}>
                    <td style={{ whiteSpace: "normal" }}>{f.name}</td>
                    <td className="numero">{(f.size / 1e6).toFixed(1)} MB</td>
                    <td>
                      <button
                        className="secundaria"
                        onClick={() => setArquivos((a) => a.filter((_, j) => j !== i))}
                      >
                        Remover
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
              <tfoot>
                <tr>
                  <td>{arquivos.length} arquivo(s)</td>
                  <td className="numero">{totalMB.toFixed(1)} MB</td>
                  <td />
                </tr>
              </tfoot>
            </table>

            <div className="linha" style={{ marginTop: "1rem" }}>
              <div className="estreito">
                <label htmlFor="mes-ref">Mês de referência</label>
                <input
                  id="mes-ref"
                  type="month"
                  value={mes}
                  onChange={(e) => setMes(e.target.value)}
                />
              </div>
              <div style={{ flex: "2 1 20rem" }}>
                <p className="ajuda" style={{ margin: 0 }}>
                  Serve para avisar quando a data lida cair fora de {nomeMes(mes)} ± 1 mês —
                  o ano é o campo que mais erra em cupom apagado.
                </p>
              </div>
              <div style={{ flex: "0 0 auto" }}>
                <button
                  className="acao"
                  disabled={importar.isPending || !sessao.leitura_habilitada}
                  onClick={() => importar.mutate()}
                >
                  {importar.isPending ? "Lendo os cupons…" : "Importar e ler"}
                </button>
              </div>
            </div>

            {importar.isPending && (
              <div className="aviso">
                Lendo {arquivos.length} arquivo(s) com o modelo{" "}
                <strong>{sessao.modelo}</strong>. Um PDF de 20 páginas leva alguns minutos —
                não feche esta aba.
              </div>
            )}
          </>
        )}

        {importar.isError && <div className="erro">{(importar.error as Error).message}</div>}
      </div>

      {importar.data && <Resultado imp={importar.data} onRevisar={onRevisar} />}
    </>
  );
}

function Resultado({
  imp,
  onRevisar,
}: {
  imp: Importacao;
  onRevisar: (id: number) => void;
}) {
  return (
    <div className="cartao">
      <h2>Resultado do lote #{imp.id}</h2>

      <p>
        {imp.total_paginas} página(s): <strong>{imp.lidas}</strong> lida(s),{" "}
        <strong>{imp.com_pendencia}</strong> com aviso,{" "}
        <strong>{imp.duplicadas}</strong> já existente(s),{" "}
        <strong>{imp.erros}</strong> com erro.
      </p>

      {imp.lidas > 0 && (
        <div className="aviso">
          Os cupons lidos entraram como <strong>pendentes</strong>. Nada vai para a
          cobrança antes de ser confirmado na aba Revisão.
        </div>
      )}

      <div className="rolagem">
        <table>
          <thead>
            <tr>
              <th>Origem</th>
              <th>Situação</th>
              <th>Cupom</th>
              <th>Cliente</th>
              <th className="numero">Total</th>
              <th>Observações</th>
            </tr>
          </thead>
          <tbody>
            {imp.detalhe.map((d, i) => (
              <tr key={i}>
                <td>
                  {d.arquivo}
                  {d.pagina > 0 && ` p.${d.pagina}`}
                </td>
                <td>
                  <span
                    className={`etiqueta ${
                      d.situacao === "lido"
                        ? "confirmado"
                        : d.situacao === "erro"
                          ? "rejeitado"
                          : "pendente"
                    }`}
                  >
                    {d.situacao === "lido"
                      ? "Lido"
                      : d.situacao === "duplicado"
                        ? "Já existia"
                        : "Erro"}
                  </span>
                </td>
                <td>
                  {d.cupom_id ? (
                    <button className="secundaria" onClick={() => onRevisar(d.cupom_id!)}>
                      {d.numero || "sem número"} — revisar
                    </button>
                  ) : (
                    d.numero || "—"
                  )}
                </td>
                <td style={{ whiteSpace: "normal" }}>{d.cliente || "—"}</td>
                <td className="numero">{d.total !== undefined ? reais(d.total) : "—"}</td>
                <td style={{ whiteSpace: "normal", maxWidth: "32rem" }}>
                  {d.erro && <div className="erro" style={{ margin: 0 }}>{d.erro}</div>}
                  {d.avisos && d.avisos.length > 0 && (
                    <ul style={{ margin: 0, paddingLeft: "1.2rem" }}>
                      {d.avisos.map((a, j) => (
                        <li key={j}>{a}</li>
                      ))}
                    </ul>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}
