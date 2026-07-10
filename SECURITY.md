# Seguridad

## Modelo

AnchorDTL asume operadores registrados, rutas DTL identificadas por origen y
destino, y garantías denominadas en un único activo de liquidación. Las
obligaciones reservan capacidad de garantía antes de activarse y se cierran por
settlement, penalización o reconciliación.

## Invariantes Esperadas

- Una ruta activa debe estar asociada a un operador activo.
- Una obligación financiada debe estar vinculada a una garantía del mismo
  operador y activo.
- Las penalizaciones no deben superar la exposición pendiente declarada.
- La reconciliación solo debe cerrar rutas con cobertura suficiente según la
  vista de solvencia.
- Los eventos y snapshots deben permitir reconstruir el historial operativo.

## Validaciones Automatizadas

La suite pública valida:

- ciclo de vida de ruta con settlement y cierre;
- accounting de penalización sobre una ruta;
- alertas operativas de monitorización;
- serialización y lectura de snapshots.

## Dependencias

El proyecto usa únicamente la biblioteca estándar de Go. Dependabot está
configurado para `gomod` y GitHub Actions.

## Alcance De Revisión

Áreas recomendadas para auditoría:

- cálculo proporcional de penalizaciones;
- consistencia entre garantía global y exposición por ruta;
- cierre de rutas y reconciliación;
- orden de actualización de obligaciones, slots y eventos;
- reportes de solvencia usados por operadores.

## Reporte Interno

Los reportes deben incluir impacto económico, precondiciones, ruta de
reproducción, cambios de estado observados y propuesta de mitigación con tests
regresivos.

