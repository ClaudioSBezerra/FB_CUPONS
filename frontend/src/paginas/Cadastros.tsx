import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, type Cliente, type Produto, type Unidade } from "../api";
import { cnpj as fmtCNPJ, numeroDeTexto, reais } from "../formato";

/**
 * Cadastros edita clientes e o catálogo de produtos.
 *
 * Produtos com confirmado = false aparecem em destaque: são códigos que a
 * leitura encontrou mas cujo nome ainda precisa ser confirmado com o cliente.
 */
export function Cadastros({ papel }: { papel: "admin" | "operador" }) {
  return (
    <>
      <Produtos />
      <Clientes />
      {papel === "admin" && <ZerarMovimento />}
    </>
  );
}

/**
 * ZerarMovimento apaga cupons, itens, fotos, importações e o log de leituras,
 * para repetir a importação de um PDF durante os testes.
 *
 * Exige digitar APAGAR: a importação deduplica imagens por SHA-256, então sem
 * limpar as fotos o mesmo arquivo seria recusado como "já importada" — o que
 * torna esta ação frequente em teste e, por isso, fácil de clicar sem querer.
 */
function ZerarMovimento() {
  const query = useQueryClient();
  const [palavra, setPalavra] = useState("");
  const [aberto, setAberto] = useState(false);

  const zerar = useMutation({
    mutationFn: api.zerarMovimento,
    onSuccess: () => {
      setPalavra("");
      setAberto(false);
      query.invalidateQueries();
    },
  });

  return (
    <div className="cartao" style={{ borderColor: "var(--erro)" }}>
      <h2 style={{ color: "var(--erro)" }}>Zerar movimento</h2>

      <p className="ajuda">
        Apaga <strong>todos os cupons, itens, fotos, importações e o log de leituras</strong>.
        Não tem volta. Clientes, catálogo de produtos e usuários são preservados.
      </p>
      <p className="ajuda">
        Serve para repetir a importação de um PDF: como as imagens são deduplicadas
        por conteúdo, sem limpar as fotos o mesmo arquivo é recusado como já
        importado.
      </p>

      {!aberto ? (
        <button className="secundaria perigo" onClick={() => setAberto(true)}>
          Zerar movimento…
        </button>
      ) : (
        <form
          className="linha"
          style={{ alignItems: "flex-end" }}
          onSubmit={(e) => {
            e.preventDefault();
            if (palavra === "APAGAR") zerar.mutate();
          }}
        >
          <div>
            <label htmlFor="z-conf">
              Digite <strong>APAGAR</strong> para confirmar
            </label>
            <input
              id="z-conf"
              value={palavra}
              onChange={(e) => setPalavra(e.target.value)}
              autoComplete="off"
            />
          </div>
          <div style={{ flex: "0 0 auto" }}>
            <button
              className="secundaria perigo"
              type="submit"
              disabled={palavra !== "APAGAR" || zerar.isPending}
            >
              {zerar.isPending ? "Apagando…" : "Apagar agora"}
            </button>{" "}
            <button
              className="secundaria"
              type="button"
              onClick={() => {
                setAberto(false);
                setPalavra("");
              }}
            >
              Cancelar
            </button>
          </div>
        </form>
      )}

      {zerar.isError && <div className="erro">{(zerar.error as Error).message}</div>}

      {zerar.data && (
        <div className="sucesso">
          Apagados: {zerar.data.apagado.cupons} cupom(ns), {zerar.data.apagado.itens} item(ns),{" "}
          {zerar.data.apagado.fotos} foto(s), {zerar.data.apagado.importacoes} importação(ões),{" "}
          {zerar.data.apagado.leituras} leitura(s). Preservados:{" "}
          {zerar.data.preservado.join(", ")}.
        </div>
      )}
    </div>
  );
}

function Produtos() {
  const query = useQueryClient();
  const produtos = useQuery({ queryKey: ["produtos"], queryFn: api.produtos });
  const [editando, setEditando] = useState<Produto | null>(null);

  const salvar = useMutation({
    mutationFn: (p: Produto) => (p.id ? api.salvarProduto(p) : api.criarProduto(p)),
    onSuccess: () => {
      setEditando(null);
      query.invalidateQueries({ queryKey: ["produtos"] });
    },
  });

  const lista = produtos.data ?? [];
  const aConfirmar = lista.filter((p) => !p.confirmado);

  return (
    <div className="cartao">
      <h2>Catálogo de produtos</h2>
      <p className="ajuda">
        O preço aqui é só referência de validação — o preço que vale é o impresso no
        cupom.
      </p>

      {aConfirmar.length > 0 && (
        <div className="aviso">
          <strong>{aConfirmar.length} produto(s) a confirmar</strong> — códigos que
          apareceram em cupons mas cujo nome ainda não foi conferido com o cliente:{" "}
          {aConfirmar.map((p) => p.codigo || "sem código").join(", ")}.
        </div>
      )}

      {produtos.isError && <div className="erro">{(produtos.error as Error).message}</div>}
      {salvar.isError && <div className="erro">{(salvar.error as Error).message}</div>}

      <button
        className="secundaria"
        style={{ marginBottom: "0.75rem" }}
        onClick={() =>
          setEditando({
            id: 0, codigo: "", descricao: "", unidade: "UND",
            preco_ref: null, confirmado: true,
          })
        }
      >
        + Novo produto
      </button>

      {editando && (
        <FormProduto
          produto={editando}
          salvando={salvar.isPending}
          onCancelar={() => setEditando(null)}
          onSalvar={(p) => salvar.mutate(p)}
        />
      )}

      <div className="rolagem">
        <table>
          <thead>
            <tr>
              <th>Código</th>
              <th>Produto</th>
              <th>Un.</th>
              <th className="numero">Preço ref.</th>
              <th>Situação</th>
              <th />
            </tr>
          </thead>
          <tbody>
            {lista.map((p) => (
              <tr key={p.id}>
                <td>{p.codigo || "—"}</td>
                <td style={{ whiteSpace: "normal" }}>{p.descricao}</td>
                <td>{p.unidade}</td>
                <td className="numero">{p.preco_ref === null ? "—" : reais(p.preco_ref)}</td>
                <td>
                  <span className={`etiqueta ${p.confirmado ? "confirmado" : "pendente"}`}>
                    {p.confirmado ? "confirmado" : "a confirmar"}
                  </span>
                </td>
                <td>
                  <button className="secundaria" onClick={() => setEditando(p)}>
                    Editar
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}

function FormProduto({
  produto,
  salvando,
  onCancelar,
  onSalvar,
}: {
  produto: Produto;
  salvando: boolean;
  onCancelar: () => void;
  onSalvar: (p: Produto) => void;
}) {
  const [codigo, setCodigo] = useState(produto.codigo);
  const [descricao, setDescricao] = useState(produto.descricao);
  const [unidade, setUnidade] = useState<Unidade>(produto.unidade);
  const [preco, setPreco] = useState(
    produto.preco_ref === null ? "" : String(produto.preco_ref).replace(".", ","),
  );
  const [confirmado, setConfirmado] = useState(produto.confirmado);

  return (
    <form
      className="linha"
      style={{ marginBottom: "1rem", alignItems: "flex-end" }}
      onSubmit={(e) => {
        e.preventDefault();
        onSalvar({
          ...produto,
          codigo: codigo.trim(),
          descricao: descricao.trim(),
          unidade,
          preco_ref: preco.trim() === "" ? null : numeroDeTexto(preco),
          confirmado,
        });
      }}
    >
      <div className="estreito">
        <label htmlFor="p-codigo">Código</label>
        <input id="p-codigo" value={codigo} onChange={(e) => setCodigo(e.target.value)} />
      </div>
      <div style={{ flex: "2 1 18rem" }}>
        <label htmlFor="p-desc">Descrição</label>
        <input id="p-desc" required value={descricao} onChange={(e) => setDescricao(e.target.value)} />
      </div>
      <div className="estreito">
        <label htmlFor="p-un">Unidade</label>
        <select id="p-un" value={unidade} onChange={(e) => setUnidade(e.target.value as Unidade)}>
          <option value="UND">UND</option>
          <option value="KG">KG</option>
        </select>
      </div>
      <div className="estreito">
        <label htmlFor="p-preco">Preço ref.</label>
        <input id="p-preco" value={preco} onChange={(e) => setPreco(e.target.value)} />
      </div>
      <div className="estreito">
        <label htmlFor="p-conf">
          <input
            id="p-conf"
            type="checkbox"
            checked={confirmado}
            onChange={(e) => setConfirmado(e.target.checked)}
            style={{ width: "auto", marginRight: "0.35rem" }}
          />
          Confirmado
        </label>
      </div>
      <div style={{ flex: "0 0 auto" }}>
        <button className="acao" type="submit" disabled={salvando}>
          {salvando ? "Salvando…" : "Salvar"}
        </button>{" "}
        <button className="secundaria" type="button" onClick={onCancelar}>
          Cancelar
        </button>
      </div>
    </form>
  );
}

function Clientes() {
  const query = useQueryClient();
  const clientes = useQuery({ queryKey: ["clientes"], queryFn: api.clientes });
  const [editando, setEditando] = useState<Cliente | null>(null);

  const salvar = useMutation({
    mutationFn: (c: Cliente) => (c.id ? api.salvarCliente(c) : api.criarCliente(c)),
    onSuccess: () => {
      setEditando(null);
      query.invalidateQueries({ queryKey: ["clientes"] });
    },
  });

  return (
    <div className="cartao">
      <h2>Clientes</h2>
      <p className="ajuda">
        O CNPJ é conferido pelo dígito verificador: um CNPJ errado aqui atrapalharia a
        sugestão de correção das leituras.
      </p>

      {clientes.isError && <div className="erro">{(clientes.error as Error).message}</div>}
      {salvar.isError && <div className="erro">{(salvar.error as Error).message}</div>}

      <button
        className="secundaria"
        style={{ marginBottom: "0.75rem" }}
        onClick={() => setEditando({ id: 0, nome: "", cnpj: "", ativo: true, obs: "" })}
      >
        + Novo cliente
      </button>

      {editando && (
        <FormCliente
          cliente={editando}
          salvando={salvar.isPending}
          onCancelar={() => setEditando(null)}
          onSalvar={(c) => salvar.mutate(c)}
        />
      )}

      <div className="rolagem">
        <table>
          <thead>
            <tr>
              <th>Nome</th>
              <th>CNPJ</th>
              <th>Situação</th>
              <th>Observação</th>
              <th />
            </tr>
          </thead>
          <tbody>
            {(clientes.data ?? []).map((c) => (
              <tr key={c.id}>
                <td style={{ whiteSpace: "normal" }}>{c.nome}</td>
                <td>{fmtCNPJ(c.cnpj)}</td>
                <td>
                  <span className={`etiqueta ${c.ativo ? "confirmado" : "rejeitado"}`}>
                    {c.ativo ? "ativo" : "inativo"}
                  </span>
                </td>
                <td style={{ whiteSpace: "normal" }}>{c.obs || "—"}</td>
                <td>
                  <button className="secundaria" onClick={() => setEditando(c)}>
                    Editar
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}

function FormCliente({
  cliente,
  salvando,
  onCancelar,
  onSalvar,
}: {
  cliente: Cliente;
  salvando: boolean;
  onCancelar: () => void;
  onSalvar: (c: Cliente) => void;
}) {
  const [nome, setNome] = useState(cliente.nome);
  const [cnpj, setCnpj] = useState(fmtCNPJ(cliente.cnpj) === "—" ? "" : fmtCNPJ(cliente.cnpj));
  const [ativo, setAtivo] = useState(cliente.ativo);
  const [obs, setObs] = useState(cliente.obs ?? "");

  return (
    <form
      className="linha"
      style={{ marginBottom: "1rem", alignItems: "flex-end" }}
      onSubmit={(e) => {
        e.preventDefault();
        onSalvar({ ...cliente, nome: nome.trim(), cnpj: cnpj.replace(/\D/g, ""), ativo, obs: obs.trim() });
      }}
    >
      <div style={{ flex: "2 1 18rem" }}>
        <label htmlFor="cl-nome">Nome</label>
        <input id="cl-nome" required value={nome} onChange={(e) => setNome(e.target.value)} />
      </div>
      <div>
        <label htmlFor="cl-cnpj">CNPJ</label>
        <input
          id="cl-cnpj"
          required
          placeholder="00.000.000/0000-00"
          value={cnpj}
          onChange={(e) => setCnpj(e.target.value)}
        />
      </div>
      <div>
        <label htmlFor="cl-obs">Observação</label>
        <input id="cl-obs" value={obs} onChange={(e) => setObs(e.target.value)} />
      </div>
      <div className="estreito">
        <label htmlFor="cl-ativo">
          <input
            id="cl-ativo"
            type="checkbox"
            checked={ativo}
            onChange={(e) => setAtivo(e.target.checked)}
            style={{ width: "auto", marginRight: "0.35rem" }}
          />
          Ativo
        </label>
      </div>
      <div style={{ flex: "0 0 auto" }}>
        <button className="acao" type="submit" disabled={salvando}>
          {salvando ? "Salvando…" : "Salvar"}
        </button>{" "}
        <button className="secundaria" type="button" onClick={onCancelar}>
          Cancelar
        </button>
      </div>
    </form>
  );
}
