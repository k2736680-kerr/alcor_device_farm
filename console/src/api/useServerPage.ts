import { useState } from 'react'

/** Local state for a server-side paginated table. */
export function useServerPage(initialSize = 20) {
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(initialSize)
  return {
    page,
    pageSize,
    onPageChange: (nextPage: number, nextSize: number) => {
      setPage(nextPage)
      setPageSize(nextSize)
    },
  }
}
