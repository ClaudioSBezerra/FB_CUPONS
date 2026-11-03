// Formatação pt-BR. Toda exibição de valor passa por aqui.

const dinheiro = new Intl.NumberFormat("pt-BR", {
  style: "currency",
  currency: "BRL",
});

const numero = new Intl.NumberFormat("pt-BR", {
  minimumFractionDigits: 0,
  maximumFractionDigits: 3,
});

export function reais(v: number | null | undefined): string {
  if (v === null || v === undefined) return "—";
  return dinheiro.format(v);
}

export function quantidade(v: number | null | undefined): string {
  if (v === null || v === undefined) return "—";
  return numero.format(v);
}

/**
 * dataBR converte "AAAA-MM-DD" em "DD/MM/AAAA" por recorte de texto.
 *
 * Deliberadamente NÃO usa new Date(): o construtor interpreta a string como
 * UTC e, em fuso negativo, mostra o dia anterior — foi exatamente esse o bug
 * de datas do protótipo.
 */
export function dataBR(iso: string | null | undefined): string {
  if (!iso) return "—";
  const partes = iso.slice(0, 10).split("-");
  if (partes.length !== 3) return iso;
  return `${partes[2]}/${partes[1]}/${partes[0]}`;
}

/** isoDeBR faz o caminho inverso, aceitando "DD/MM/AAAA". */
export function isoDeBR(br: string): string {
  const partes = br.trim().split("/");
  if (partes.length !== 3) return br.trim();
  const [d, m, a] = partes;
  return `${a}-${(m ?? "").padStart(2, "0")}-${(d ?? "").padStart(2, "0")}`;
}

export function cnpj(digitos: string | null | undefined): string {
  if (!digitos) return "—";
  const d = digitos.replace(/\D/g, "");
  if (d.length !== 14) return digitos;
  return `${d.slice(0, 2)}.${d.slice(2, 5)}.${d.slice(5, 8)}/${d.slice(8, 12)}-${d.slice(12)}`;
}

/** mesAtual devolve a competência de hoje no formato AAAA-MM. */
export function mesAtual(): string {
  const hoje = new Date();
  return `${hoje.getFullYear()}-${String(hoje.getMonth() + 1).padStart(2, "0")}`;
}

/** nomeMes devolve "outubro/2026" a partir de "2026-10". */
export function nomeMes(mes: string): string {
  const [a, m] = mes.split("-");
  const nomes = [
    "janeiro", "fevereiro", "março", "abril", "maio", "junho",
    "julho", "agosto", "setembro", "outubro", "novembro", "dezembro",
  ];
  const i = Number(m) - 1;
  if (!a || i < 0 || i > 11) return mes;
  return `${nomes[i]}/${a}`;
}

/**
 * numeroDeTexto interpreta o que o operador digita num campo de valor,
 * aceitando "1.234,56" e "1234.56". Devolve null quando não é número.
 */
export function numeroDeTexto(texto: string): number | null {
  const limpo = texto.trim().replace(/[R$\s]/g, "");
  if (!limpo) return null;

  const ultimaVirgula = limpo.lastIndexOf(",");
  const ultimoPonto = limpo.lastIndexOf(".");

  let normalizado: string;
  if (ultimaVirgula > ultimoPonto) {
    normalizado = limpo.replace(/\./g, "").replace(",", ".");
  } else if (ultimoPonto > ultimaVirgula) {
    normalizado = limpo.replace(/,/g, "");
  } else {
    normalizado = limpo;
  }

  const n = Number(normalizado);
  return Number.isFinite(n) ? n : null;
}
