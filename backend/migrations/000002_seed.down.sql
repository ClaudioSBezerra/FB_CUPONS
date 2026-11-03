-- Remove apenas as linhas do seed que nunca foram usadas em um cupom, para
-- não apagar cadastro que o operador editou ou que já tem movimento.
DELETE FROM produtos p
 WHERE p.codigo IN ('20771','22121','2755','1594','1645','3629','941','210','836','673',
                    '213','3636','2431','383','1207','232','2045','1485','2077','1534',
                    '288','110','1930','2156','3000')
   AND NOT EXISTS (SELECT 1 FROM cupom_itens i WHERE i.codigo = p.codigo);

DELETE FROM clientes c
 WHERE c.cnpj IN ('37678845000736','37678845000302','02709992000156')
   AND NOT EXISTS (SELECT 1 FROM cupons k WHERE k.cliente_id = c.id);
