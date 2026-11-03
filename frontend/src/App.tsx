import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, ErroAPI, type Sessao } from "./api";
import { Login } from "./paginas/Login";
import { Importar } from "./paginas/Importar";
import { Revisao } from "./paginas/Revisao";
import { Cupons } from "./paginas/Cupons";
import { Cobranca } from "./paginas/Cobranca";
import { Cadastros } from "./paginas/Cadastros";

type Aba = "importar" | "revisao" | "cupons" | "cobranca" | "cadastros";

const ABAS: { id: Aba; rotulo: string }[] = [
  { id: "importar", rotulo: "Importar" },
  { id: "revisao", rotulo: "Revisão" },
  { id: "cupons", rotulo: "Cupons" },
  { id: "cobranca", rotulo: "Cobrança" },
  { id: "cadastros", rotulo: "Cadastros" },
];

export function App() {
  const [aba, setAba] = useState<Aba>("importar");
  // cupomEmRevisao permite "Importar → revisar este cupom" sem perder o lugar.
  const [cupomEmRevisao, setCupomEmRevisao] = useState<number | null>(null);

  const sessao = useQuery<Sessao>({
    queryKey: ["me"],
    queryFn: api.eu,
    retry: false,
  });

  if (sessao.isLoading) {
    return <p className="vazio">Carregando…</p>;
  }

  if (sessao.isError) {
    const err = sessao.error;
    if (err instanceof ErroAPI && err.status === 401) {
      return <Login onEntrou={() => sessao.refetch()} />;
    }
    return (
      <main>
        <div className="erro">
          Não foi possível falar com o servidor: {(err as Error).message}
        </div>
      </main>
    );
  }

  const dados = sessao.data!;

  function irParaRevisao(cupomID: number) {
    setCupomEmRevisao(cupomID);
    setAba("revisao");
  }

  return (
    <>
      <Topo
        sessao={dados}
        aba={aba}
        onAba={(a) => {
          setAba(a);
          if (a !== "revisao") setCupomEmRevisao(null);
        }}
      />
      <main>
        {/* Cada aba é desmontada ao sair, menos a Revisão quando há um cupom
            aberto — assim a edição em andamento não se perde. */}
        {aba === "importar" && <Importar sessao={dados} onRevisar={irParaRevisao} />}
        {aba === "revisao" && (
          <Revisao sessao={dados} cupomInicial={cupomEmRevisao} />
        )}
        {aba === "cupons" && <Cupons onAbrir={irParaRevisao} />}
        {aba === "cobranca" && <Cobranca />}
        {aba === "cadastros" && <Cadastros />}
      </main>
    </>
  );
}

function Topo({
  sessao,
  aba,
  onAba,
}: {
  sessao: Sessao;
  aba: Aba;
  onAba: (a: Aba) => void;
}) {
  const query = useQueryClient();
  const sair = useMutation({
    mutationFn: api.sair,
    onSuccess: () => query.clear(),
  });

  return (
    <header className="topo">
      <h1>Cupons ITD</h1>
      <nav className="abas">
        {ABAS.map((a) => (
          <button
            key={a.id}
            onClick={() => onAba(a.id)}
            aria-current={aba === a.id ? "page" : undefined}
          >
            {a.rotulo}
          </button>
        ))}
      </nav>
      <span className="espaco" />
      <span className="usuario">
        {sessao.usuario.email}
        {!sessao.leitura_habilitada && " · leitura automática desligada"}
      </span>
      <button className="secundaria" onClick={() => sair.mutate()}>
        Sair
      </button>
    </header>
  );
}
