// Cliente da API do Cupons ITD.
//
// A sessão é um cookie httpOnly, então toda requisição vai com
// credentials: "include" e nenhum token encosta em JavaScript.

// A API é SEMPRE na mesma origem, em /api.
//
// Não há URL configurável de propósito. Uma URL absoluta embutida em tempo de
// build é uma armadilha: o valor viaja dentro do JS e, se estiver errado, o
// navegador de quem acessa tenta falar com outro endereço — foi assim que um
// "http://localhost:8080" vazou para produção e todo fetch falhou.
//
// Nos três modos de execução o caminho /api já chega na API:
//   * npm run dev  -> o proxy do vite.config.ts encaminha
//   * contêiner    -> o nginx do frontend repassa
//   * produção     -> idem, e o cookie de sessão fica first-party
const BASE = "";

export type Status = "pendente" | "confirmado" | "rejeitado";
export type Unidade = "UND" | "KG";

export interface Usuario {
  id: number;
  email: string;
  nome: string;
  papel: "admin" | "operador";
  ativo: boolean;
}

export interface Cliente {
  id: number;
  nome: string;
  cnpj: string;
  ativo: boolean;
  obs?: string;
}

export interface Produto {
  id: number;
  codigo: string;
  descricao: string;
  unidade: Unidade;
  preco_ref: number | null;
  confirmado: boolean;
}

export interface Item {
  id?: number;
  ordem: number;
  cod: string;
  prod: string;
  un: Unidade | "";
  qtd: number;
  vun: number;
  vtot: number;
  incertos: string[];
}

export interface Cupom {
  id: number;
  numero: string;
  data: string | null;
  cliente_id: number | null;
  cliente: string;
  cnpj_cliente: string;
  cnpj_emitente: string;
  total: number;
  status: Status;
  itens: Item[];
  incertos: string[];
  avisos: string[];
  fonte: string;
  obs: string;
  foto_id: number | null;
}

export interface CupomDetalhe extends Cupom {
  pode_confirmar: boolean;
  bloqueios: string[];
  soma_itens: number;
  parecidos?: string[];
}

export interface DetalhePagina {
  arquivo: string;
  pagina: number;
  situacao: "lido" | "duplicado" | "erro";
  cupom_id?: number;
  numero?: string;
  cliente?: string;
  total?: number;
  avisos?: string[];
  erro?: string;
}

export interface Importacao {
  id: number;
  arquivos: string[];
  total_paginas: number;
  lidas: number;
  com_pendencia: number;
  duplicadas: number;
  erros: number;
  status: "processando" | "concluida" | "falhou";
  detalhe: DetalhePagina[];
}

export interface LinhaCobranca {
  mes: string;
  cliente_id: number;
  cliente: string;
  cnpj_cliente: string;
  codigo: string;
  produto: string;
  unidade: Unidade;
  qtd_total: number;
  num_cupons: number;
  valor_total: number;
}

export interface Subtotal {
  cliente_id: number;
  cliente: string;
  cnpj_cliente: string;
  num_cupons: number;
  total: number;
}

export interface Cobranca {
  mes: string;
  linhas: LinhaCobranca[];
  subtotais: Subtotal[];
  total: number;
  pendentes: number;
  rejeitados: number;
}

export interface Sessao {
  usuario: Usuario;
  leitura_habilitada: boolean;
  modelo: string;
  modelo_forte: string;
}

/** ErroAPI carrega a mensagem em português vinda do servidor. */
export class ErroAPI extends Error {
  constructor(
    public status: number,
    mensagem: string,
    public detalhes: string[] = [],
  ) {
    super(mensagem);
    this.name = "ErroAPI";
  }
}

async function pedir<T>(caminho: string, init: RequestInit = {}): Promise<T> {
  const resp = await fetch(`${BASE}/api${caminho}`, {
    ...init,
    credentials: "include",
    headers:
      init.body instanceof FormData
        ? init.headers
        : { "Content-Type": "application/json", ...(init.headers ?? {}) },
  });

  if (!resp.ok) {
    let mensagem = `Falha na requisição (HTTP ${resp.status})`;
    let detalhes: string[] = [];
    try {
      const corpo = await resp.json();
      if (corpo?.erro) mensagem = corpo.erro;
      if (Array.isArray(corpo?.detalhes)) detalhes = corpo.detalhes;
    } catch {
      // resposta sem JSON: fica a mensagem genérica
    }
    throw new ErroAPI(resp.status, mensagem, detalhes);
  }

  if (resp.status === 204) return undefined as T;
  return (await resp.json()) as T;
}

export const api = {
  // --- sessão ---
  entrar: (email: string, senha: string) =>
    pedir<{ usuario: Usuario }>("/auth/login", {
      method: "POST",
      body: JSON.stringify({ email, senha }),
    }),
  sair: () => pedir<void>("/auth/logout", { method: "POST" }),
  eu: () => pedir<Sessao>("/me"),

  // --- importação ---
  importar: (arquivos: File[], mes: string) => {
    const form = new FormData();
    for (const a of arquivos) form.append("arquivos", a);
    if (mes) form.append("mes", mes);
    return pedir<Importacao>("/importacoes", { method: "POST", body: form });
  },
  importacao: (id: number) => pedir<Importacao>(`/importacoes/${id}`),

  // --- cupons ---
  cupons: (filtro: {
    mes?: string;
    cliente_id?: number;
    status?: Status | "";
    numero?: string;
  }) => {
    const q = new URLSearchParams();
    if (filtro.mes) q.set("mes", filtro.mes);
    if (filtro.cliente_id) q.set("cliente_id", String(filtro.cliente_id));
    if (filtro.status) q.set("status", filtro.status);
    if (filtro.numero) q.set("numero", filtro.numero);
    return pedir<Cupom[]>(`/cupons?${q}`);
  },
  cupom: (id: number) => pedir<CupomDetalhe>(`/cupons/${id}`),
  salvarCupom: (id: number, corpo: unknown) =>
    pedir<CupomDetalhe>(`/cupons/${id}`, { method: "PUT", body: JSON.stringify(corpo) }),
  confirmar: (id: number, obs = "", forcar = false) =>
    pedir<CupomDetalhe>(`/cupons/${id}/confirmar`, {
      method: "POST",
      body: JSON.stringify({ obs, forcar_divergencia: forcar }),
    }),
  rejeitar: (id: number, obs = "") =>
    pedir<{ ok: boolean }>(`/cupons/${id}/rejeitar`, {
      method: "POST",
      body: JSON.stringify({ obs }),
    }),
  reprocessar: (id: number, forte: boolean) =>
    pedir<CupomDetalhe>(`/cupons/${id}/reprocessar`, {
      method: "POST",
      body: JSON.stringify({ forte }),
    }),
  urlFoto: (id: number) => `${BASE}/api/cupons/${id}/foto`,

  // --- cadastros ---
  clientes: () => pedir<Cliente[]>("/clientes"),
  criarCliente: (c: Partial<Cliente>) =>
    pedir<Cliente>("/clientes", { method: "POST", body: JSON.stringify(c) }),
  salvarCliente: (c: Cliente) =>
    pedir<Cliente>(`/clientes/${c.id}`, { method: "PUT", body: JSON.stringify(c) }),

  produtos: () => pedir<Produto[]>("/produtos"),
  criarProduto: (p: Partial<Produto>) =>
    pedir<Produto>("/produtos", { method: "POST", body: JSON.stringify(p) }),
  salvarProduto: (p: Produto) =>
    pedir<Produto>(`/produtos/${p.id}`, { method: "PUT", body: JSON.stringify(p) }),

  // --- cobrança ---
  cobranca: (mes: string, clienteID = 0) => {
    const q = new URLSearchParams({ mes });
    if (clienteID) q.set("cliente_id", String(clienteID));
    return pedir<Cobranca>(`/cobranca?${q}`);
  },
  urlExcel: (mes: string, clienteID = 0) => {
    const q = new URLSearchParams({ mes });
    if (clienteID) q.set("cliente_id", String(clienteID));
    return `${BASE}/api/cobranca/export.xlsx?${q}`;
  },
};
