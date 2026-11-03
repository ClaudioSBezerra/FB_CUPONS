import { describe, expect, it } from "vitest";
import { cnpj, dataBR, isoDeBR, nomeMes, numeroDeTexto, quantidade, reais } from "./formato";

describe("dataBR", () => {
  it("não desloca a data por causa de fuso", () => {
    // O bug do protótipo: new Date('2026-10-16') em fuso negativo mostra 15/10.
    expect(dataBR("2026-10-16")).toBe("16/10/2026");
    expect(dataBR("2026-01-01")).toBe("01/01/2026");
    expect(dataBR("2026-12-31")).toBe("31/12/2026");
  });

  it("aceita data com hora e vazio", () => {
    expect(dataBR("2026-10-16T00:00:00Z")).toBe("16/10/2026");
    expect(dataBR(null)).toBe("—");
    expect(dataBR("")).toBe("—");
  });
});

describe("isoDeBR", () => {
  it("volta ao formato ISO", () => {
    expect(isoDeBR("16/10/2026")).toBe("2026-10-16");
    expect(isoDeBR("1/1/2026")).toBe("2026-01-01");
  });
});

describe("numeroDeTexto", () => {
  it("aceita o que o operador digita", () => {
    expect(numeroDeTexto("1.234,56")).toBe(1234.56);
    expect(numeroDeTexto("1234.56")).toBe(1234.56);
    expect(numeroDeTexto("1,234.56")).toBe(1234.56);
    expect(numeroDeTexto("R$ 435,00")).toBe(435);
    expect(numeroDeTexto("435")).toBe(435);
    expect(numeroDeTexto("0,01")).toBe(0.01);
    expect(numeroDeTexto("1,340")).toBe(1.34);
  });

  it("devolve null para o que não é número", () => {
    expect(numeroDeTexto("")).toBeNull();
    expect(numeroDeTexto("  ")).toBeNull();
    expect(numeroDeTexto("abc")).toBeNull();
  });
});

describe("reais", () => {
  it("formata em pt-BR", () => {
    expect(reais(7225.77)).toMatch(/7\.225,77/);
    expect(reais(18726.23)).toMatch(/18\.726,23/);
    expect(reais(0)).toMatch(/0,00/);
    expect(reais(null)).toBe("—");
  });
});

describe("quantidade", () => {
  it("mostra até 3 decimais, sem zeros inúteis", () => {
    expect(quantidade(120)).toBe("120");
    expect(quantidade(2480)).toBe("2.480");
    expect(quantidade(1.34)).toBe("1,34");
  });
});

describe("cnpj", () => {
  it("formata e preserva entrada inválida", () => {
    expect(cnpj("29220487000108")).toBe("29.220.487/0001-08");
    expect(cnpj("37678845000302")).toBe("37.678.845/0003-02");
    expect(cnpj("123")).toBe("123");
    expect(cnpj(null)).toBe("—");
  });
});

describe("nomeMes", () => {
  it("escreve o mês em português", () => {
    expect(nomeMes("2026-10")).toBe("outubro/2026");
    expect(nomeMes("2026-03")).toBe("março/2026");
  });
});
